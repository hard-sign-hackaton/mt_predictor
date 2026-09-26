package catalog

import (
	"math"
	"time"
)

type MatchStatus string

const (
	MatchMatched         MatchStatus = "matched"
	MatchMatchedSpatial  MatchStatus = "matched_spatial"
	MatchUnmappedUnit    MatchStatus = "unmapped_unit"
	MatchNoSchedule      MatchStatus = "no_schedule"
	MatchNoActivePattern MatchStatus = "no_active_pattern"
	MatchInvalidLocation MatchStatus = "invalid_location"
	MatchOffRoute        MatchStatus = "off_route"
)

const (
	assignmentMargin = 15 * time.Minute
	maxRouteDistance = 500.0
	moscowLonScale   = 62400.0
	latScale         = 111200.0
)

type Telemetry struct {
	UnitID      uint32
	Lon         float64
	Lat         float64
	Speed       float64
	Heading     float64
	EventTime   time.Time
	ReceiveTime time.Time
	Valid       bool
	Historical  bool
}

type MatchResult struct {
	Status              MatchStatus `json:"status"`
	UnitID              uint32      `json:"unit_id"`
	TRID                int64       `json:"tr_id,omitempty"`
	RoutePatternID      string      `json:"route_pattern_id,omitempty"`
	OccurrenceID        string      `json:"occurrence_id,omitempty"`
	PreviousStopID      string      `json:"previous_stop_id,omitempty"`
	PreviousStopAddress string      `json:"previous_stop_address,omitempty"`
	NextStopID          string      `json:"next_stop_id,omitempty"`
	NextStopAddress     string      `json:"next_stop_address,omitempty"`
	NextActionItemID    int64       `json:"next_action_item_id,omitempty"`
	DistanceToRouteM    float64     `json:"distance_to_route_m,omitempty"`
	GeometryQuality     string      `json:"geometry_quality,omitempty"`
}

type Matcher struct{ catalog *Catalog }

func NewMatcher(catalog *Catalog) *Matcher { return &Matcher{catalog: catalog} }

func (m *Matcher) Match(telemetry Telemetry) MatchResult {
	result := MatchResult{Status: MatchUnmappedUnit, UnitID: telemetry.UnitID}
	trID, exists := m.catalog.TRIDForUnit(telemetry.UnitID)
	if !exists {
		return result
	}
	result.TRID = trID
	if !telemetry.Valid || math.IsNaN(telemetry.Lon) || math.IsNaN(telemetry.Lat) {
		result.Status = MatchInvalidLocation
		return result
	}
	assignments := m.catalog.AssignmentsForTR(trID)
	if len(assignments) == 0 {
		result.Status = MatchNoSchedule
		return result
	}
	candidates := make([]RouteAssignment, 0, 2)
	for _, assignment := range assignments {
		if !telemetry.EventTime.Before(assignment.ValidFrom.Add(-assignmentMargin)) &&
			!telemetry.EventTime.After(assignment.ValidTo.Add(assignmentMargin)) {
			candidates = append(candidates, assignment)
		}
	}
	if len(candidates) == 0 {
		result.Status = MatchNoActivePattern
		return result
	}

	bestDistance := math.Inf(1)
	var best RouteAssignment
	var bestPattern RoutePattern
	for _, assignment := range candidates {
		pattern, ok := m.catalog.Pattern(assignment.RoutePatternID)
		if !ok {
			continue
		}
		distance := distanceToPolyline(telemetry.Lon, telemetry.Lat, pattern.Polyline)
		if distance < bestDistance {
			bestDistance, best, bestPattern = distance, assignment, pattern
		}
	}
	if math.IsInf(bestDistance, 1) {
		result.Status = MatchNoActivePattern
		return result
	}
	result.RoutePatternID = best.RoutePatternID
	result.OccurrenceID = best.OccurrenceID
	result.GeometryQuality = bestPattern.GeometryQuality
	result.DistanceToRouteM = math.Round(bestDistance*10) / 10
	if bestDistance > maxRouteDistance {
		result.Status = MatchOffRoute
		return result
	}
	segment := nearestScheduleSegment(telemetry, best.Events)
	if segment < 0 || segment+1 >= len(best.Events) {
		result.Status = MatchOffRoute
		return result
	}
	previous, next := best.Events[segment], best.Events[segment+1]
	result.Status = MatchMatched
	result.PreviousStopID = previous.StopID
	result.NextStopID = next.StopID
	result.NextActionItemID = next.ActionItemID
	if stop, ok := m.catalog.stopByID[previous.StopID]; ok {
		result.PreviousStopAddress = stop.Address
	}
	if stop, ok := m.catalog.stopByID[next.StopID]; ok {
		result.NextStopAddress = stop.Address
	}
	return result
}

// MatchLive сначала выполняет строгое сопоставление по времени расписания.
// Если календарный период демонстрационного расписания уже прошёл, метод
// выбирает среди паттернов известного TRID ближайшую геометрию. Такой результат
// получает отдельный статус matched_spatial и не выдаётся за временной match.
func (m *Matcher) MatchLive(telemetry Telemetry) MatchResult {
	result := m.Match(telemetry)
	if result.Status != MatchNoActivePattern {
		return result
	}
	return m.matchSpatial(telemetry, result.TRID)
}

func (m *Matcher) matchSpatial(telemetry Telemetry, trID int64) MatchResult {
	result := MatchResult{Status: MatchNoActivePattern, UnitID: telemetry.UnitID, TRID: trID}
	bestDistance := math.Inf(1)
	var best RouteAssignment
	var bestPattern RoutePattern
	seenPatterns := make(map[string]struct{})
	for _, assignment := range m.catalog.AssignmentsForTR(trID) {
		if _, exists := seenPatterns[assignment.RoutePatternID]; exists {
			continue
		}
		seenPatterns[assignment.RoutePatternID] = struct{}{}
		pattern, ok := m.catalog.Pattern(assignment.RoutePatternID)
		if !ok {
			continue
		}
		distance := distanceToPolyline(telemetry.Lon, telemetry.Lat, pattern.Polyline)
		if distance < bestDistance {
			bestDistance, best, bestPattern = distance, assignment, pattern
		}
	}
	if math.IsInf(bestDistance, 1) {
		return result
	}
	result.RoutePatternID = best.RoutePatternID
	result.OccurrenceID = best.OccurrenceID
	result.GeometryQuality = bestPattern.GeometryQuality
	result.DistanceToRouteM = math.Round(bestDistance*10) / 10
	if bestDistance > maxRouteDistance {
		result.Status = MatchOffRoute
		return result
	}
	segment := nearestScheduleSegment(telemetry, best.Events)
	if segment < 0 || segment+1 >= len(best.Events) {
		result.Status = MatchOffRoute
		return result
	}
	previous, next := best.Events[segment], best.Events[segment+1]
	result.Status = MatchMatchedSpatial
	result.PreviousStopID = previous.StopID
	result.NextStopID = next.StopID
	result.NextActionItemID = next.ActionItemID
	if stop, ok := m.catalog.stopByID[previous.StopID]; ok {
		result.PreviousStopAddress = stop.Address
	}
	if stop, ok := m.catalog.stopByID[next.StopID]; ok {
		result.NextStopAddress = stop.Address
	}
	return result
}

func nearestScheduleSegment(telemetry Telemetry, events []ScheduleEvent) int {
	bestScore, best := math.Inf(1), -1
	for index := 0; index+1 < len(events); index++ {
		a, b := events[index], events[index+1]
		distance := pointSegmentDistance(telemetry.Lon, telemetry.Lat, Point{a.Lon, a.Lat}, Point{b.Lon, b.Lat})
		score := distance
		if telemetry.Speed > 5 {
			bearing := segmentBearing(a.Lon, a.Lat, b.Lon, b.Lat)
			delta := math.Abs(bearing - telemetry.Heading)
			if delta > 180 {
				delta = 360 - delta
			}
			score += delta / 180 * 100
		}
		// Плановое время используется только как дополнительный критерий:
		// координата важнее, поскольку ТС уже может идти с задержкой.
		midpoint := a.PlannedAt.Add(b.PlannedAt.Sub(a.PlannedAt) / 2)
		score += math.Min(math.Abs(telemetry.EventTime.Sub(midpoint).Minutes()), 60) * 0.5
		if score < bestScore {
			bestScore, best = score, index
		}
	}
	return best
}

func distanceToPolyline(lon, lat float64, points []Point) float64 {
	if len(points) == 0 {
		return math.Inf(1)
	}
	if len(points) == 1 {
		return math.Hypot((lon-points[0][0])*moscowLonScale, (lat-points[0][1])*latScale)
	}
	best := math.Inf(1)
	for index := 0; index+1 < len(points); index++ {
		best = math.Min(best, pointSegmentDistance(lon, lat, points[index], points[index+1]))
	}
	return best
}

func pointSegmentDistance(lon, lat float64, a, b Point) float64 {
	px, py := lon*moscowLonScale, lat*latScale
	ax, ay := a[0]*moscowLonScale, a[1]*latScale
	bx, by := b[0]*moscowLonScale, b[1]*latScale
	dx, dy := bx-ax, by-ay
	denominator := dx*dx + dy*dy
	if denominator == 0 {
		return math.Hypot(px-ax, py-ay)
	}
	t := math.Max(0, math.Min(1, ((px-ax)*dx+(py-ay)*dy)/denominator))
	return math.Hypot(px-(ax+t*dx), py-(ay+t*dy))
}

func segmentBearing(aLon, aLat, bLon, bLat float64) float64 {
	dx := (bLon - aLon) * moscowLonScale
	dy := (bLat - aLat) * latScale
	degrees := math.Atan2(dx, dy) * 180 / math.Pi
	if degrees < 0 {
		degrees += 360
	}
	return degrees
}

// LiveState допускает исторические пакеты для дальнейшего хранения истории,
// но не позволяет им откатывать маркер и текущее сопоставление на dashboard.
type LiveState struct {
	matcher *Matcher
	latest  map[uint32]Telemetry
	matches map[uint32]MatchResult
}

func NewLiveState(matcher *Matcher) *LiveState {
	return &LiveState{matcher: matcher, latest: map[uint32]Telemetry{}, matches: map[uint32]MatchResult{}}
}

func (s *LiveState) Apply(telemetry Telemetry) (MatchResult, bool) {
	if current, exists := s.latest[telemetry.UnitID]; exists && telemetry.EventTime.Before(current.EventTime) {
		return s.matches[telemetry.UnitID], false
	}
	match := s.matcher.Match(telemetry)
	s.latest[telemetry.UnitID] = telemetry
	s.matches[telemetry.UnitID] = match
	return match, true
}
