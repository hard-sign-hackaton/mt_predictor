package catalog

import (
	"mt_predictor/models"
	"testing"
	"time"
)

func testCatalog(t *testing.T) *Catalog {
	t.Helper()
	start := time.Date(2026, 1, 6, 10, 0, 0, 0, time.UTC)
	result := &Catalog{
		SchemaVersion: 1,
		VehicleBindings: []VehicleBinding{
			{UnitID: 100, TRID: 200, HasSchedule: true},
			{UnitID: 101, TRID: 201, HasSchedule: false},
		},
		Stops: []Stop{
			{StopID: "a", Lon: 37.60, Lat: 55.70, Address: "A"},
			{StopID: "b", Lon: 37.61, Lat: 55.70, Address: "B"},
			{StopID: "c", Lon: 37.62, Lat: 55.70, Address: "C"},
		},
		RoutePatterns: []RoutePattern{{
			RoutePatternID: "route_pattern_test", StopIDs: []string{"a", "b", "c"},
			Polyline: []Point{{37.60, 55.70}, {37.61, 55.70}, {37.62, 55.70}}, GeometryQuality: "gps_repeated",
		}},
		Assignments: []RouteAssignment{{
			OccurrenceID: "occ_test", TRID: 200, RoutePatternID: "route_pattern_test",
			ValidFrom: start, ValidTo: start.Add(20 * time.Minute),
			Events: []ScheduleEvent{
				{ActionItemID: 1, StopID: "a", PlannedAt: start, Lon: 37.60, Lat: 55.70},
				{ActionItemID: 2, StopID: "b", PlannedAt: start.Add(10 * time.Minute), Lon: 37.61, Lat: 55.70},
				{ActionItemID: 3, StopID: "c", PlannedAt: start.Add(20 * time.Minute), Lon: 37.62, Lat: 55.70},
			},
		}},
	}
	if err := result.buildIndexes(); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestLoadGeneratedCatalog(t *testing.T) {
	catalog, err := Load("../data/generated/route_catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	if trID, ok := catalog.TRIDForUnit(786201); !ok || trID != 122658 {
		t.Fatalf("unexpected binding: trID=%d ok=%v", trID, ok)
	}
}

func TestMatchReturnsRouteAndAdjacentStops(t *testing.T) {
	matcher := NewMatcher(testCatalog(t))
	result := matcher.Match(Telemetry{
		UnitID: 100, Lon: 37.605, Lat: 55.70, Speed: 20, Heading: 90,
		EventTime: time.Date(2026, 1, 6, 10, 5, 0, 0, time.UTC), Valid: true,
	})
	if result.Status != MatchMatched || result.RoutePatternID != "route_pattern_test" {
		t.Fatalf("unexpected match: %+v", result)
	}
	if result.PreviousStopID != "a" || result.NextStopID != "b" || result.NextActionItemID != 2 {
		t.Fatalf("unexpected segment: %+v", result)
	}
}

func TestMatchFailureStates(t *testing.T) {
	matcher := NewMatcher(testCatalog(t))
	now := time.Date(2026, 1, 6, 10, 5, 0, 0, time.UTC)
	tests := []struct {
		name  string
		input Telemetry
		want  MatchStatus
	}{
		{"unmapped", Telemetry{UnitID: 999, Valid: true, EventTime: now}, MatchUnmappedUnit},
		{"invalid", Telemetry{UnitID: 100, Valid: false, EventTime: now}, MatchInvalidLocation},
		{"no schedule", Telemetry{UnitID: 101, Valid: true, EventTime: now}, MatchNoSchedule},
		{"no active pattern", Telemetry{UnitID: 100, Lon: 37.605, Lat: 55.70, Valid: true, EventTime: now.Add(2 * time.Hour)}, MatchNoActivePattern},
		{"off route", Telemetry{UnitID: 100, Lon: 38.0, Lat: 56.0, Valid: true, EventTime: now}, MatchOffRoute},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := matcher.Match(test.input).Status; got != test.want {
				t.Fatalf("got %s, want %s", got, test.want)
			}
		})
	}
}

func TestLiveStateDoesNotRewindOnOlderEvent(t *testing.T) {
	state := NewLiveState(NewMatcher(testCatalog(t)))
	newer := Telemetry{UnitID: 100, Lon: 37.615, Lat: 55.70, Valid: true, EventTime: time.Date(2026, 1, 6, 10, 15, 0, 0, time.UTC)}
	older := Telemetry{UnitID: 100, Lon: 37.605, Lat: 55.70, Valid: true, Historical: true, EventTime: newer.EventTime.Add(-10 * time.Minute)}
	first, applied := state.Apply(newer)
	if !applied {
		t.Fatal("newer event was not applied")
	}
	second, applied := state.Apply(older)
	if applied || second.NextStopID != first.NextStopID {
		t.Fatalf("older historical event rewound state: first=%+v second=%+v", first, second)
	}
}

func TestDashboardInitConvertsGeneratedCatalog(t *testing.T) {
	catalog := testCatalog(t)
	result := catalog.DashboardInit("catalog-test", models.RiskThresholds{
		WatchDelaySeconds: 180, HighDelaySeconds: 420,
	})
	if result.CatalogVersion != "catalog-test" || len(result.Routes) != 1 || len(result.Occurrences) != 1 {
		t.Fatalf("unexpected init payload: %+v", result)
	}
	if result.Routes[0].ID != "route_pattern_test" || result.Occurrences[0].Calls[1].ActionItemID != 2 {
		t.Fatalf("route catalog was converted incorrectly: %+v", result)
	}
}

func TestVehicleStateUsesOnlyTelemetryAndMatchFacts(t *testing.T) {
	now := time.Date(2026, 1, 6, 10, 5, 0, 0, time.UTC)
	telemetry := Telemetry{
		UnitID: 100, Lon: 37.605, Lat: 55.70, Speed: 20, Heading: 90,
		EventTime: now, ReceiveTime: now.Add(time.Second), Valid: true,
	}
	match := NewMatcher(testCatalog(t)).Match(telemetry)
	result := VehicleState(telemetry, match, models.TelemetryLive)
	if result.TRID == nil || *result.TRID != 200 || result.RoutePatternID == nil || *result.RoutePatternID != "route_pattern_test" {
		t.Fatalf("unexpected vehicle state: %+v", result)
	}
	if result.NextStop == nil || result.NextStop.ID != "b" || result.NextActionItemID == nil || *result.NextActionItemID != 2 {
		t.Fatalf("stop context was not transferred: %+v", result)
	}
}
