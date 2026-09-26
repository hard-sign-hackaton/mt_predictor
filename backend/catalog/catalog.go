package catalog

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"
)

// Catalog is static, derived reference data. RoutePatternID is an internal
// geometry identifier and must not be presented as an official route number.
type Catalog struct {
	SchemaVersion   int               `json:"schema_version"`
	SourceHashes    map[string]string `json:"source_hashes"`
	VehicleBindings []VehicleBinding  `json:"vehicle_bindings"`
	Stops           []Stop            `json:"stops"`
	RoutePatterns   []RoutePattern    `json:"route_patterns"`
	Assignments     []RouteAssignment `json:"assignments"`
	unitToTR        map[uint32]int64
	stopByID        map[string]Stop
	patternByID     map[string]RoutePattern
	assignmentsByTR map[int64][]RouteAssignment
}

type VehicleBinding struct {
	UnitID       uint32   `json:"unit_id"`
	TRID         int64    `json:"tr_id"`
	SourceSplits []string `json:"source_splits"`
	HasSchedule  bool     `json:"has_schedule"`
	Synthetic    bool     `json:"synthetic"`
}

type Stop struct {
	StopID  string  `json:"stop_id"`
	Lon     float64 `json:"lon"`
	Lat     float64 `json:"lat"`
	Address string  `json:"address"`
}

type Point [2]float64

type RoutePattern struct {
	RoutePatternID  string          `json:"route_pattern_id"`
	StopIDs         []string        `json:"stop_ids"`
	Polyline        []Point         `json:"polyline"`
	FrequentPoints  []FrequentPoint `json:"frequent_points"`
	GeometryQuality string          `json:"geometry_quality"`
	Quality         PatternQuality  `json:"quality"`
}

type FrequentPoint struct {
	Lon             float64 `json:"lon"`
	Lat             float64 `json:"lat"`
	OccurrenceCount int     `json:"occurrence_count"`
}

type PatternQuality struct {
	OccurrenceCount        int      `json:"occurrence_count"`
	GoodGPSOccurrenceCount int      `json:"good_gps_occurrence_count"`
	BestStopCoverage       float64  `json:"best_stop_coverage"`
	MedianStopToGPSMeters  *float64 `json:"median_stop_to_gps_m"`
	MaxGPSJumpMeters       float64  `json:"max_gps_jump_m"`
	PolylinePointCount     int      `json:"polyline_point_count"`
}

type RouteAssignment struct {
	OccurrenceID   string          `json:"occurrence_id"`
	TRID           int64           `json:"tr_id"`
	RoutePatternID string          `json:"route_pattern_id"`
	ValidFrom      time.Time       `json:"valid_from"`
	ValidTo        time.Time       `json:"valid_to"`
	Events         []ScheduleEvent `json:"events"`
}

type ScheduleEvent struct {
	ActionItemID int64     `json:"action_item_id"`
	StopID       string    `json:"stop_id"`
	PlannedAt    time.Time `json:"planned_at"`
	Lon          float64   `json:"lon"`
	Lat          float64   `json:"lat"`
}

func Load(path string) (*Catalog, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read route catalog: %w", err)
	}
	var result Catalog
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("decode route catalog: %w", err)
	}
	if result.SchemaVersion != 1 {
		return nil, fmt.Errorf("unsupported route catalog schema_version=%d", result.SchemaVersion)
	}
	if err := result.buildIndexes(); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Catalog) buildIndexes() error {
	c.unitToTR = make(map[uint32]int64, len(c.VehicleBindings))
	c.stopByID = make(map[string]Stop, len(c.Stops))
	c.patternByID = make(map[string]RoutePattern, len(c.RoutePatterns))
	c.assignmentsByTR = make(map[int64][]RouteAssignment)
	for _, binding := range c.VehicleBindings {
		if previous, exists := c.unitToTR[binding.UnitID]; exists && previous != binding.TRID {
			return fmt.Errorf("unit_id %d has conflicting tr_id values", binding.UnitID)
		}
		c.unitToTR[binding.UnitID] = binding.TRID
	}
	for _, stop := range c.Stops {
		c.stopByID[stop.StopID] = stop
	}
	for _, pattern := range c.RoutePatterns {
		c.patternByID[pattern.RoutePatternID] = pattern
	}
	for _, assignment := range c.Assignments {
		if _, exists := c.patternByID[assignment.RoutePatternID]; !exists {
			return fmt.Errorf("assignment %s references missing pattern %s", assignment.OccurrenceID, assignment.RoutePatternID)
		}
		c.assignmentsByTR[assignment.TRID] = append(c.assignmentsByTR[assignment.TRID], assignment)
	}
	for trID := range c.assignmentsByTR {
		sort.Slice(c.assignmentsByTR[trID], func(i, j int) bool {
			return c.assignmentsByTR[trID][i].ValidFrom.Before(c.assignmentsByTR[trID][j].ValidFrom)
		})
	}
	return nil
}

func (c *Catalog) TRIDForUnit(unitID uint32) (int64, bool) {
	trID, ok := c.unitToTR[unitID]
	return trID, ok
}

func (c *Catalog) AssignmentsForTR(trID int64) []RouteAssignment {
	return c.assignmentsByTR[trID]
}

func (c *Catalog) Pattern(patternID string) (RoutePattern, bool) {
	pattern, ok := c.patternByID[patternID]
	return pattern, ok
}
