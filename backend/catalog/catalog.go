package catalog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Catalog — статические производные справочные данные, загружаемые один раз
// при старте. RoutePatternID является внутренним идентификатором геометрии и
// не должен показываться как официальный номер московского маршрута.
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
	assignmentByID  map[string]RouteAssignment
	assignmentsByTR map[int64][]RouteAssignment
}

// VehicleBinding — соответствие между unit_id телеметрии и tr_id расписания.
// SourceSplits перечисляет файлы-источники, из которых взята связка.
type VehicleBinding struct {
	UnitID       uint32   `json:"unit_id"`
	TRID         int64    `json:"tr_id"`
	SourceSplits []string `json:"source_splits"`
	HasSchedule  bool     `json:"has_schedule"`
	Synthetic    bool     `json:"synthetic"`
}

// Stop — физическая остановка, дедуплицированная по округлённым координатам.
// Address заполняется из справочника и может отсутствовать.
type Stop struct {
	StopID  string  `json:"stop_id"`
	Lon     float64 `json:"lon"`
	Lat     float64 `json:"lat"`
	Address string  `json:"address"`
}

// Point — пара координат в полилинии паттерна, в порядке долготы и широты.
type Point [2]float64

// RoutePattern — повторяющийся путь, вычисленный по расписанию и GPS.
// Поля OfficialRoute помечены json:"-" и заполняются из внешнего справочника.
type RoutePattern struct {
	RoutePatternID       string          `json:"route_pattern_id"`
	OfficialRouteID      string          `json:"-"`
	OfficialRouteName    string          `json:"-"`
	OfficialMatchQuality string          `json:"-"`
	StopIDs              []string        `json:"stop_ids"`
	Polyline             []Point         `json:"polyline"`
	FrequentPoints       []FrequentPoint `json:"frequent_points"`
	GeometryQuality      string          `json:"geometry_quality"`
	Quality              PatternQuality  `json:"quality"`
}

type publicTransportReference struct {
	SchemaVersion int                             `json:"schema_version"`
	Stops         map[string]publicStopReference  `json:"stops"`
	Routes        map[string]publicRouteReference `json:"routes"`
}

type publicStopReference struct {
	Name string `json:"name"`
}

type publicRouteReference struct {
	Number       string `json:"number"`
	Name         string `json:"name"`
	MatchQuality string `json:"match_quality"`
}

// FrequentPoint — повторяющаяся GPS-ячейка, посчитанная для диагностики.
type FrequentPoint struct {
	Lon             float64 `json:"lon"`
	Lat             float64 `json:"lat"`
	OccurrenceCount int     `json:"occurrence_count"`
}

// PatternQuality — числовые показатели качества вычисленной линии паттерна.
// MedianStopToGPSMeters отсутствует, если пригодного GPS-прохода не было.
type PatternQuality struct {
	OccurrenceCount        int      `json:"occurrence_count"`
	GoodGPSOccurrenceCount int      `json:"good_gps_occurrence_count"`
	BestStopCoverage       float64  `json:"best_stop_coverage"`
	MedianStopToGPSMeters  *float64 `json:"median_stop_to_gps_m"`
	MaxGPSJumpMeters       float64  `json:"max_gps_jump_m"`
	PolylinePointCount     int      `json:"polyline_point_count"`
}

// RouteAssignment — один рейс расписания в границах ValidFrom и ValidTo.
type RouteAssignment struct {
	OccurrenceID   string          `json:"occurrence_id"`
	TRID           int64           `json:"tr_id"`
	RoutePatternID string          `json:"route_pattern_id"`
	ValidFrom      time.Time       `json:"valid_from"`
	ValidTo        time.Time       `json:"valid_to"`
	Events         []ScheduleEvent `json:"events"`
}

// ScheduleEvent — плановое прибытие на остановку в составе рейса.
// Lon и Lat скопированы из остановки, чтобы выбор цели не требовал поиска.
type ScheduleEvent struct {
	ActionItemID int64     `json:"action_item_id"`
	StopID       string    `json:"stop_id"`
	PlannedAt    time.Time `json:"planned_at"`
	Lon          float64   `json:"lon"`
	Lat          float64   `json:"lat"`
}

// Load читает каталог, проверяет версию схемы, подставляет внешний справочник
// маршрутов и строит индексы. Отсутствующий public_transport_reference.json
// допустим: тогда официальные номера и названия остаются пустыми.
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
	if err := result.applyPublicTransportReference(filepath.Join(filepath.Dir(path), "public_transport_reference.json")); err != nil {
		return nil, err
	}
	if err := result.buildIndexes(); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Catalog) applyPublicTransportReference(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read public transport reference: %w", err)
	}
	var reference publicTransportReference
	if err := json.Unmarshal(data, &reference); err != nil {
		return fmt.Errorf("decode public transport reference: %w", err)
	}
	if reference.SchemaVersion != 1 {
		return fmt.Errorf("unsupported public transport reference schema_version=%d", reference.SchemaVersion)
	}
	for index := range c.Stops {
		if public, ok := reference.Stops[c.Stops[index].StopID]; ok && public.Name != "" {
			c.Stops[index].Address = public.Name
		}
	}
	for index := range c.RoutePatterns {
		if public, ok := reference.Routes[c.RoutePatterns[index].RoutePatternID]; ok {
			c.RoutePatterns[index].OfficialRouteID = public.Number
			c.RoutePatterns[index].OfficialRouteName = public.Name
			c.RoutePatterns[index].OfficialMatchQuality = public.MatchQuality
		}
	}
	return nil
}

func (c *Catalog) buildIndexes() error {
	c.unitToTR = make(map[uint32]int64, len(c.VehicleBindings))
	c.stopByID = make(map[string]Stop, len(c.Stops))
	c.patternByID = make(map[string]RoutePattern, len(c.RoutePatterns))
	c.assignmentByID = make(map[string]RouteAssignment, len(c.Assignments))
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
		c.assignmentByID[assignment.OccurrenceID] = assignment
		c.assignmentsByTR[assignment.TRID] = append(c.assignmentsByTR[assignment.TRID], assignment)
	}
	for trID := range c.assignmentsByTR {
		sort.Slice(c.assignmentsByTR[trID], func(i, j int) bool {
			return c.assignmentsByTR[trID][i].ValidFrom.Before(c.assignmentsByTR[trID][j].ValidFrom)
		})
	}
	return nil
}

// TRIDForUnit возвращает tr_id по unit_id телеметрии.
func (c *Catalog) TRIDForUnit(unitID uint32) (int64, bool) {
	trID, ok := c.unitToTR[unitID]
	return trID, ok
}

// AssignmentsForTR возвращает все рейсы tr_id, отсортированные по времени начала.
func (c *Catalog) AssignmentsForTR(trID int64) []RouteAssignment {
	return c.assignmentsByTR[trID]
}

// Pattern возвращает вычисленный паттерн по его идентификатору геометрии.
func (c *Catalog) Pattern(patternID string) (RoutePattern, bool) {
	pattern, ok := c.patternByID[patternID]
	return pattern, ok
}

// Assignment возвращает рейс с упорядоченными событиями расписания. Online-
// конвейер использует его для выбора цели ML в окне 10–15 минут.
func (c *Catalog) Assignment(occurrenceID string) (RouteAssignment, bool) {
	assignment, ok := c.assignmentByID[occurrenceID]
	return assignment, ok
}

// Stop возвращает физическую остановку для читаемого описания прогноза.
func (c *Catalog) Stop(stopID string) (Stop, bool) {
	stop, ok := c.stopByID[stopID]
	return stop, ok
}
