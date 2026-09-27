package catalog

import (
	"math"
	"testing"
	"time"

	"mt_predictor/models"
)

func whatIfTestStart() time.Time {
	return time.Date(2026, 1, 6, 10, 0, 0, 0, time.UTC)
}

// whatIfTestCatalog строит рейс из шести событий с тремя повторяющимися
// остановками, чтобы средний интервал обслуживания был вычислим, и второй
// рейс того же tr_id для проверки конфликта.
func whatIfTestCatalog(t *testing.T) *Catalog {
	t.Helper()
	start := whatIfTestStart()
	events := make([]ScheduleEvent, 0, 6)
	future := make([]ScheduleEvent, 0, 6)
	sequence := []ScheduleEvent{
		{ActionItemID: 1, StopID: "a", Lon: 37.60, Lat: 55.70},
		{ActionItemID: 2, StopID: "b", Lon: 37.61, Lat: 55.70},
		{ActionItemID: 3, StopID: "c", Lon: 37.62, Lat: 55.70},
		{ActionItemID: 4, StopID: "a", Lon: 37.60, Lat: 55.70},
		{ActionItemID: 5, StopID: "b", Lon: 37.61, Lat: 55.70},
		{ActionItemID: 6, StopID: "c", Lon: 37.62, Lat: 55.70},
	}
	for index, event := range sequence {
		event.PlannedAt = start.Add(time.Duration(index) * 10 * time.Minute)
		events = append(events, event)
		shifted := event
		shifted.PlannedAt = event.PlannedAt.Add(24 * time.Hour)
		future = append(future, shifted)
	}
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
			RoutePatternID: "pattern_test", StopIDs: []string{"a", "b", "c"},
			Polyline: []Point{{37.60, 55.70}, {37.61, 55.70}, {37.62, 55.70}}, GeometryQuality: "gps_repeated",
		}},
		Assignments: []RouteAssignment{
			{
				OccurrenceID: "occ_scheduled", TRID: 200, RoutePatternID: "pattern_test",
				ValidFrom: start, ValidTo: start.Add(2 * time.Hour), Events: events,
			},
			{
				OccurrenceID: "occ_other", TRID: 999, RoutePatternID: "pattern_test",
				ValidFrom: start, ValidTo: start.Add(2 * time.Hour), Events: events,
			},
			{
				OccurrenceID: "occ_future", TRID: 999, RoutePatternID: "pattern_test",
				ValidFrom: start.Add(24 * time.Hour), ValidTo: start.Add(26 * time.Hour), Events: future,
			},
			{
				OccurrenceID: "occ_empty", TRID: 999, RoutePatternID: "pattern_test",
				ValidFrom: start, ValidTo: start.Add(time.Hour),
			},
		},
	}
	if err := result.buildIndexes(); err != nil {
		t.Fatal(err)
	}
	return result
}

func whatIfTestCandidate() WhatIfCandidate {
	return WhatIfCandidate{
		Known: true, TRID: 201, HasSchedule: false, MatchStatus: models.MatchNoSchedule,
		Freshness: models.TelemetryLive,
		Position:  &models.GeoPoint{Lon: 37.60, Lat: 55.70},
		EventTime: whatIfTestStart(),
	}
}

func TestWhatIfRejectsUnknownOccurrence(t *testing.T) {
	catalog := whatIfTestCatalog(t)
	if _, ok := catalog.WhatIf(WhatIfRequest{OccurrenceID: "missing"}, whatIfTestCandidate(), whatIfTestStart()); ok {
		t.Fatal("unknown occurrence must be reported as not found")
	}
}

func TestWhatIfDistanceUsesMoscowProjection(t *testing.T) {
	got := whatIfDistanceMeters(37.60, 55.70, 37.61, 55.70)
	if math.Abs(got-624.0) > 0.001 {
		t.Fatalf("expected 624 meters for 0.01 lon, got %v", got)
	}
	if zero := whatIfDistanceMeters(37.60, 55.70, 37.60, 55.70); zero != 0 {
		t.Fatalf("expected zero distance, got %v", zero)
	}
}

func TestWhatIfDeadheadVerdicts(t *testing.T) {
	start := whatIfTestStart()
	// Кандидаты различаются только расстоянием до границы разбиения, которая
	// в этом тесте всегда стоит на остановке «a» в момент start. Позиция
	// зафиксирована в момент start, если не указано иное.
	near := whatIfTestCandidate()
	tight := whatIfTestCandidate()
	tight.Position = &models.GeoPoint{Lon: 37.64, Lat: 55.70} // ~2.5 км
	far := whatIfTestCandidate()
	far.Position = &models.GeoPoint{Lon: 37.00, Lat: 55.70} // ~37 км
	stale := whatIfTestCandidate()
	stale.Freshness = models.TelemetryStale
	// Граница разбиения позади позиции кандидата: момент фиксации позиции
	// позже планового события.
	observedAfter := whatIfTestCandidate()
	observedAfter.EventTime = start.Add(3 * time.Minute)
	// Граница разбиения дальше горизонта решения.
	observedLongBefore := whatIfTestCandidate()
	observedLongBefore.EventTime = start.Add(-48 * time.Hour)

	cases := []struct {
		name      string
		candidate WhatIfCandidate
		verdict   models.WhatIfVerdict
		wantSlack bool
	}{
		// Граница впереди позиции: запас считается и сравнивается с перегоном.
		{"feasible", near, models.WhatIfFeasible, true},
		{"tight", tight, models.WhatIfTight, true},
		{"infeasible", far, models.WhatIfInfeasible, true},
		// Граница вне горизонта решения: запас не вычисляется, потому что
		// разность моментов выглядит числом, но смысла не имеет.
		{"run in past", observedAfter, models.WhatIfRunInPast, false},
		{"run too far", observedLongBefore, models.WhatIfRunTooFar, false},
		{"stale", stale, models.WhatIfStalePosition, true},
		{"no position", WhatIfCandidate{
			Known: true, TRID: 201, MatchStatus: models.MatchNoSchedule, Freshness: models.TelemetryLive,
		}, models.WhatIfNoPosition, false},
		{"unknown vehicle", WhatIfCandidate{}, models.WhatIfUnknownVehicle, false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			catalog := whatIfTestCatalog(t)
			report, ok := catalog.WhatIf(WhatIfRequest{
				UnitID: 101, OccurrenceID: "occ_other", JoinCallIndex: 0,
			}, testCase.candidate, start)
			if !ok {
				t.Fatal("expected occurrence to be found")
			}
			if report.Deadhead.Verdict == nil || *report.Deadhead.Verdict != testCase.verdict {
				t.Fatalf("expected verdict %s, got %v", testCase.verdict, *report.Deadhead.Verdict)
			}
			if got := report.Deadhead.SlackSeconds != nil; got != testCase.wantSlack {
				t.Fatalf("expected slack presence %v, got %v", testCase.wantSlack, got)
			}
		})
	}
}

// TestWhatIfSlackIgnoresWallClock фиксирует исходный дефект: запас считался как
// requiredBy − time.Now(), поэтому на историческом расписании (январь против
// реальных часов) он доходил до −6329 ч — правдоподобное число, не значащее
// ничего. Опорное время — момент фиксации позиции, а стенные часы не должны
// влиять на сценарий вовсе.
func TestWhatIfSlackIgnoresWallClock(t *testing.T) {
	start := whatIfTestStart()
	catalog := whatIfTestCatalog(t)
	candidate := whatIfTestCandidate()
	request := WhatIfRequest{UnitID: 101, OccurrenceID: "occ_other", JoinCallIndex: 1}

	reference, ok := catalog.WhatIf(request, candidate, start)
	if !ok {
		t.Fatal("expected occurrence to be found")
	}
	// Восемь месяцев позже расписания — как в демо на реальных часах.
	later, ok := catalog.WhatIf(request, candidate, start.Add(263*24*time.Hour))
	if !ok {
		t.Fatal("expected occurrence to be found")
	}
	if reference.Deadhead.SlackSeconds == nil || later.Deadhead.SlackSeconds == nil {
		t.Fatal("expected slack for both calls")
	}
	if *reference.Deadhead.SlackSeconds != *later.Deadhead.SlackSeconds {
		t.Fatalf("wall clock must not change slack: %v vs %v", *reference.Deadhead.SlackSeconds, *later.Deadhead.SlackSeconds)
	}
	if *reference.Deadhead.SlackSeconds <= 0 || *reference.Deadhead.SlackSeconds > 24*3600 {
		t.Fatalf("expected plausible slack, got %v hours", *reference.Deadhead.SlackSeconds/3600)
	}
}

// TestWhatIfSlackFollowsVehiclePosition: запас обязан меняться вместе с
// положением кандидата, иначе панель показывает одно и то же для всего парка.
func TestWhatIfSlackFollowsVehiclePosition(t *testing.T) {
	start := whatIfTestStart()
	catalog := whatIfTestCatalog(t)
	request := WhatIfRequest{UnitID: 101, OccurrenceID: "occ_other", JoinCallIndex: 1}
	report := func(lon float64) *float64 {
		candidate := whatIfTestCandidate()
		candidate.Position = &models.GeoPoint{Lon: lon, Lat: 55.70}
		result, ok := catalog.WhatIf(request, candidate, start)
		if !ok {
			t.Fatal("expected occurrence to be found")
		}
		return result.Deadhead.SlackSeconds
	}
	// Граница разбиения — остановка «b» в 37.61.
	atJoin, far := report(37.61), report(37.30)
	if atJoin == nil || far == nil {
		t.Fatal("expected slack in both cases")
	}
	if *atJoin <= *far {
		t.Fatalf("moving the vehicle away must reduce slack: %v vs %v", *atJoin, *far)
	}
	if *atJoin >= 0 && *far >= 0 {
		t.Fatalf("expected the distant vehicle to miss the split: %v", *far)
	}
}

func TestWhatIfMissingPositionOmitsNumbersInsteadOfZero(t *testing.T) {
	catalog := whatIfTestCatalog(t)
	candidate := WhatIfCandidate{Known: true, TRID: 201, Freshness: models.TelemetryLive}
	report, ok := catalog.WhatIf(WhatIfRequest{
		UnitID: 101, OccurrenceID: "occ_other", JoinCallIndex: 0,
	}, candidate, whatIfTestStart())
	if !ok {
		t.Fatal("expected occurrence to be found")
	}
	if report.Deadhead.Meters != nil || report.Deadhead.Seconds != nil || report.Deadhead.SlackSeconds != nil {
		t.Fatal("absent position must omit distance numbers, not report zero")
	}
	if report.Deadhead.UnavailableReason != models.WhatIfReasonNoPosition {
		t.Fatalf("expected reason %s, got %q", models.WhatIfReasonNoPosition, report.Deadhead.UnavailableReason)
	}
}

func TestWhatIfDeadheadComputesDistanceAndSpeed(t *testing.T) {
	start := whatIfTestStart()
	catalog := whatIfTestCatalog(t)
	candidate := whatIfTestCandidate()
	candidate.Position = &models.GeoPoint{Lon: 37.60, Lat: 55.70}
	report, ok := catalog.WhatIf(WhatIfRequest{
		UnitID: 101, OccurrenceID: "occ_other", JoinCallIndex: 1, EmptySpeedKmh: 30,
	}, candidate, start.Add(-10*time.Minute))
	if !ok {
		t.Fatal("expected occurrence to be found")
	}
	// Событие №1 находится в 0.01 градуса широты: 0.01 * 62400 = 624 метра.
	if report.Deadhead.Meters == nil || math.Abs(*report.Deadhead.Meters-624.0) > 0.001 {
		t.Fatalf("expected 624 meters, got %v", report.Deadhead.Meters)
	}
	// 624 м при 30 км/ч (8.3333 м/с) = 74.88 с.
	if report.Deadhead.Seconds == nil || math.Abs(*report.Deadhead.Seconds-74.88) > 0.01 {
		t.Fatalf("expected 74.88 seconds, got %v", report.Deadhead.Seconds)
	}
	if report.Deadhead.SlackSeconds == nil {
		t.Fatal("expected slack to be computed")
	}
	// Событие №1 запланировано на 10:10, позиция кандидата зафиксирована в
	// 10:00, поэтому до границы разбиения 10 минут.
	if expected := 600.0 - 74.88; math.Abs(*report.Deadhead.SlackSeconds-expected) > 0.01 {
		t.Fatalf("expected slack %v, got %v", expected, *report.Deadhead.SlackSeconds)
	}
	if report.Deadhead.AssumedEmptySpeedKmh != 30 {
		t.Fatalf("assumed speed must be reported, got %v", report.Deadhead.AssumedEmptySpeedKmh)
	}
}

func TestWhatIfClampsJoinCallIndex(t *testing.T) {
	catalog := whatIfTestCatalog(t)
	cases := map[string]struct{ requested, expected int }{
		"too large": {requested: 99, expected: 5},
		"negative":  {requested: -5, expected: 0},
		"in range":  {requested: 2, expected: 2},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			report, ok := catalog.WhatIf(WhatIfRequest{
				UnitID: 101, OccurrenceID: "occ_other", JoinCallIndex: testCase.requested,
			}, whatIfTestCandidate(), whatIfTestStart())
			if !ok {
				t.Fatal("expected occurrence to be found")
			}
			if report.Target.JoinCallIndex != testCase.expected {
				t.Fatalf("expected index %d, got %d", testCase.expected, report.Target.JoinCallIndex)
			}
		})
	}
}

func TestWhatIfEmptyOccurrenceDoesNotPanic(t *testing.T) {
	catalog := whatIfTestCatalog(t)
	report, ok := catalog.WhatIf(WhatIfRequest{
		UnitID: 101, OccurrenceID: "occ_empty", JoinCallIndex: 0,
	}, whatIfTestCandidate(), whatIfTestStart())
	if !ok {
		t.Fatal("expected occurrence to be found")
	}
	if report.Deadhead.Verdict == nil || *report.Deadhead.Verdict != models.WhatIfEmptyOccurrence {
		t.Fatalf("expected verdict %s, got %v", models.WhatIfEmptyOccurrence, report.Deadhead.Verdict)
	}
	if report.Target.TotalCalls != 0 {
		t.Fatalf("expected zero calls, got %d", report.Target.TotalCalls)
	}
}

func TestWhatIfConflictDetectsOverlappingOwnSchedule(t *testing.T) {
	catalog := whatIfTestCatalog(t)
	candidate := WhatIfCandidate{
		Known: true, TRID: 200, HasSchedule: true, MatchStatus: models.MatchMatched,
		Freshness: models.TelemetryLive, Position: &models.GeoPoint{Lon: 37.60, Lat: 55.70},
	}
	report, ok := catalog.WhatIf(WhatIfRequest{
		UnitID: 100, OccurrenceID: "occ_other", JoinCallIndex: 0,
	}, candidate, whatIfTestStart())
	if !ok {
		t.Fatal("expected occurrence to be found")
	}
	if !report.Conflict.Detected || report.Conflict.OccurrenceID != "occ_scheduled" {
		t.Fatalf("expected conflict with occ_scheduled, got %+v", report.Conflict)
	}
	if report.Conflict.TRID != 200 {
		t.Fatalf("expected conflict tr_id 200, got %d", report.Conflict.TRID)
	}
}

func TestWhatIfOwnOccurrenceIsNotAConflict(t *testing.T) {
	catalog := whatIfTestCatalog(t)
	candidate := WhatIfCandidate{
		Known: true, TRID: 200, HasSchedule: true, MatchStatus: models.MatchMatched,
		Freshness: models.TelemetryLive, Position: &models.GeoPoint{Lon: 37.60, Lat: 55.70},
	}
	report, _ := catalog.WhatIf(WhatIfRequest{
		UnitID: 100, OccurrenceID: "occ_scheduled", JoinCallIndex: 0,
	}, candidate, whatIfTestStart())
	if report.Conflict.Detected {
		t.Fatal("a vehicle's own occurrence must not be reported as a conflict")
	}
}

func TestWhatIfNoConflictOutsideWindow(t *testing.T) {
	catalog := whatIfTestCatalog(t)
	candidate := WhatIfCandidate{
		Known: true, TRID: 200, HasSchedule: true, MatchStatus: models.MatchMatched,
		Freshness: models.TelemetryLive, Position: &models.GeoPoint{Lon: 37.60, Lat: 55.70},
	}
	report, _ := catalog.WhatIf(WhatIfRequest{
		UnitID: 100, OccurrenceID: "occ_future", JoinCallIndex: 0,
	}, candidate, whatIfTestStart())
	if report.Conflict.Detected {
		t.Fatal("assignments in disjoint windows must not conflict")
	}
}

// TestWhatIfRelieveKeepsPublishedHeadway — главный инвариант честности режима
// relieve: опубликованное расписание не меняется, поэтому интервал обслуживания
// обязан остаться прежним, а выигрыш выражается только в объёме работы на ТС.
func TestWhatIfRelieveKeepsPublishedHeadway(t *testing.T) {
	catalog := whatIfTestCatalog(t)
	report, _ := catalog.WhatIf(WhatIfRequest{
		UnitID: 101, OccurrenceID: "occ_scheduled", JoinCallIndex: 2, Mode: models.WhatIfModeRelieve,
	}, whatIfTestCandidate(), whatIfTestStart())
	if report.ServiceInterval.MeanHeadwayBeforeSeconds == nil || report.ServiceInterval.MeanHeadwayAfterSeconds == nil {
		t.Fatal("expected headway to be computed")
	}
	before, after := *report.ServiceInterval.MeanHeadwayBeforeSeconds, *report.ServiceInterval.MeanHeadwayAfterSeconds
	if math.Abs(before-after) > 0.001 {
		t.Fatalf("relieve must not change headway: before=%v after=%v", before, after)
	}
	if len(report.ServiceInterval.ImprovedStops) != 0 {
		t.Fatalf("relieve must not report improved stops, got %d", len(report.ServiceInterval.ImprovedStops))
	}
	if report.Partition.CallsVehicleOne != 2 || report.Partition.CallsVehicleTwo != 4 {
		t.Fatalf("unexpected split: %d/%d", report.Partition.CallsVehicleOne, report.Partition.CallsVehicleTwo)
	}
	if report.Partition.FullRunSeconds == nil || *report.Partition.FullRunSeconds != 3000 {
		t.Fatalf("expected 3000 second run, got %v", report.Partition.FullRunSeconds)
	}
	if report.Partition.LongestVehicleRunSeconds == nil || *report.Partition.LongestVehicleRunSeconds != 1800 {
		t.Fatalf("expected longest run 1800, got %v", report.Partition.LongestVehicleRunSeconds)
	}
	// При разбиении после третьего события остановки a и b имеют визиты с обеих
	// сторон границы, а у остановки c оба визита попадают во вторую половину,
	// поэтому её обслуживает только кандидат.
	if report.Partition.StopsServedByOne != 1 || report.Partition.StopsServedByTwo != 2 {
		t.Fatalf("unexpected stop coverage: %+v", report.Partition)
	}
}

// TestWhatIfRelieveLeavesStopsOnOneSideServedByOne — остановка, все визиты
// которой попали в одну половину рейса, обслуживается ровно одним ТС.
func TestWhatIfRelieveLeavesStopsOnOneSideServedByOne(t *testing.T) {
	catalog := whatIfTestCatalog(t)
	report, _ := catalog.WhatIf(WhatIfRequest{
		UnitID: 101, OccurrenceID: "occ_scheduled", JoinCallIndex: 0, Mode: models.WhatIfModeRelieve,
	}, whatIfTestCandidate(), whatIfTestStart())
	if report.Partition.StopsServedByOne != 3 || report.Partition.StopsServedByTwo != 0 {
		t.Fatalf("unexpected stop coverage: %+v", report.Partition)
	}
	if report.Partition.CallsVehicleOne != 0 || report.Partition.CallsVehicleTwo != 6 {
		t.Fatalf("unexpected split: %+v", report.Partition)
	}
}

// TestWhatIfDuplicateHalvesHeadway — в режиме duplicate второй ТС обслуживает
// те же события, поэтому интервал действительно сокращается почти вдвое.
func TestWhatIfDuplicateHalvesHeadway(t *testing.T) {
	catalog := whatIfTestCatalog(t)
	report, _ := catalog.WhatIf(WhatIfRequest{
		UnitID: 101, OccurrenceID: "occ_scheduled", JoinCallIndex: 2, Mode: models.WhatIfModeDuplicate,
	}, whatIfTestCandidate(), whatIfTestStart())
	before, after := report.ServiceInterval.MeanHeadwayBeforeSeconds, report.ServiceInterval.MeanHeadwayAfterSeconds
	if before == nil || after == nil {
		t.Fatal("expected headway to be computed")
	}
	if *after >= *before {
		t.Fatalf("duplicate must shorten headway: before=%v after=%v", *before, *after)
	}
	if len(report.ServiceInterval.ImprovedStops) != 3 {
		t.Fatalf("expected all three stops to be improved, got %d", len(report.ServiceInterval.ImprovedStops))
	}
	// У остановки c оба визита попадают во вторую половину рейса, поэтому
	// дублируются оба: 4 визита с охватом 30 минут дают интервал 1800/3 = 600 с.
	top := report.ServiceInterval.ImprovedStops[0]
	if top.StopID != "c" {
		t.Fatalf("expected stop c to be the most improved, got %s", top.StopID)
	}
	if top.VisitsBefore != 2 || top.VisitsAfter != 4 {
		t.Fatalf("expected 2 visits before and 4 after, got %d/%d", top.VisitsBefore, top.VisitsAfter)
	}
	if top.HeadwayBeforeSeconds == nil || math.Abs(*top.HeadwayBeforeSeconds-1800) > 0.001 {
		t.Fatalf("expected headway before 1800, got %v", top.HeadwayBeforeSeconds)
	}
	if top.HeadwayAfterSeconds == nil || math.Abs(*top.HeadwayAfterSeconds-600) > 0.001 {
		t.Fatalf("expected headway after 600, got %v", top.HeadwayAfterSeconds)
	}
	if top.Reduction == nil || math.Abs(*top.Reduction-2.0/3.0) > 0.001 {
		t.Fatalf("expected reduction 2/3, got %v", top.Reduction)
	}
	if top.Address != "C" {
		t.Fatalf("expected stop address, got %q", top.Address)
	}
	// У остановки a только второй визит дублируется, поэтому её интервал
	// сокращается вдвое, а не на две трети.
	partial := report.ServiceInterval.ImprovedStops[1]
	if partial.StopID != "a" {
		t.Fatalf("expected stop a to rank second, got %s", partial.StopID)
	}
	if partial.VisitsAfter != 3 {
		t.Fatalf("expected 3 visits after for stop a, got %d", partial.VisitsAfter)
	}
	if partial.HeadwayAfterSeconds == nil || math.Abs(*partial.HeadwayAfterSeconds-900) > 0.001 {
		t.Fatalf("expected headway after 900 for stop a, got %v", partial.HeadwayAfterSeconds)
	}
	if partial.Reduction == nil || math.Abs(*partial.Reduction-0.5) > 0.001 {
		t.Fatalf("expected reduction 1/2 for stop a, got %v", partial.Reduction)
	}
}

func TestWhatIfDuplicateDoesNotShortenLongestRun(t *testing.T) {
	catalog := whatIfTestCatalog(t)
	report, _ := catalog.WhatIf(WhatIfRequest{
		UnitID: 101, OccurrenceID: "occ_scheduled", JoinCallIndex: 2, Mode: models.WhatIfModeDuplicate,
	}, whatIfTestCandidate(), whatIfTestStart())
	if report.Partition.FullRunSeconds == nil || report.Partition.LongestVehicleRunSeconds == nil {
		t.Fatal("expected run spans to be computed")
	}
	if math.Abs(*report.Partition.FullRunSeconds-*report.Partition.LongestVehicleRunSeconds) > 0.001 {
		t.Fatal("duplicate must not shorten the longest run: work is not divided")
	}
}

func TestWhatIfNeverClaimsScheduleRelief(t *testing.T) {
	catalog := whatIfTestCatalog(t)
	for _, mode := range []models.WhatIfMode{models.WhatIfModeRelieve, models.WhatIfModeDuplicate, ""} {
		report, _ := catalog.WhatIf(WhatIfRequest{
			UnitID: 101, OccurrenceID: "occ_scheduled", JoinCallIndex: 2, Mode: mode,
		}, whatIfTestCandidate(), whatIfTestStart())
		if report.ScheduleReliefSeconds != nil {
			t.Fatalf("mode %q must not report schedule relief, got %v", mode, *report.ScheduleReliefSeconds)
		}
		if report.UnavailableReason != models.WhatIfReasonNoCapacityModel {
			t.Fatalf("expected reason %s, got %q", models.WhatIfReasonNoCapacityModel, report.UnavailableReason)
		}
	}
}

func TestWhatIfUnknownModeFallsBackToRelieve(t *testing.T) {
	catalog := whatIfTestCatalog(t)
	report, _ := catalog.WhatIf(WhatIfRequest{
		UnitID: 101, OccurrenceID: "occ_scheduled", JoinCallIndex: 2, Mode: "что-то другое",
	}, whatIfTestCandidate(), whatIfTestStart())
	if report.Mode != models.WhatIfModeRelieve {
		t.Fatalf("expected fallback to relieve, got %q", report.Mode)
	}
}

func TestWhatIfTopStopsIsBounded(t *testing.T) {
	catalog := whatIfTestCatalog(t)
	report, _ := catalog.WhatIf(WhatIfRequest{
		UnitID: 101, OccurrenceID: "occ_scheduled", JoinCallIndex: 2,
		Mode: models.WhatIfModeDuplicate, TopStops: 1,
	}, whatIfTestCandidate(), whatIfTestStart())
	if len(report.ServiceInterval.ImprovedStops) != 1 {
		t.Fatalf("expected one improved stop, got %d", len(report.ServiceInterval.ImprovedStops))
	}
}

func TestWhatIfNonPositiveSpeedFallsBackToDefault(t *testing.T) {
	catalog := whatIfTestCatalog(t)
	report, _ := catalog.WhatIf(WhatIfRequest{
		UnitID: 101, OccurrenceID: "occ_scheduled", JoinCallIndex: 0, EmptySpeedKmh: -5,
	}, whatIfTestCandidate(), whatIfTestStart())
	if report.Deadhead.AssumedEmptySpeedKmh != WhatIfDefaultEmptySpeedKmh {
		t.Fatalf("expected default speed, got %v", report.Deadhead.AssumedEmptySpeedKmh)
	}
}

// TestWhatIfOnGeneratedCatalog — проверка на реальных данных: рейс делится,
// сумма покрытия остановок совпадает с числом уникальных остановок.
func TestWhatIfOnGeneratedCatalog(t *testing.T) {
	catalog, err := Load("../data/generated/route_catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	var target RouteAssignment
	for _, assignment := range catalog.Assignments {
		if len(assignment.Events) > 20 && target.OccurrenceID == "" {
			target = assignment
		}
	}
	if target.OccurrenceID == "" {
		t.Skip("no multi-event occurrence in generated catalog")
	}
	candidate := WhatIfCandidate{
		Known: true, TRID: 115106, HasSchedule: false, MatchStatus: models.MatchNoSchedule,
		Freshness: models.TelemetryLive,
		Position:  &models.GeoPoint{Lon: 37.617, Lat: 55.755},
	}
	report, ok := catalog.WhatIf(WhatIfRequest{
		UnitID: 664030, OccurrenceID: target.OccurrenceID, JoinCallIndex: len(target.Events) / 2,
		Mode: models.WhatIfModeRelieve,
	}, candidate, target.Events[0].PlannedAt.Add(-15*time.Minute))
	if !ok {
		t.Fatal("expected occurrence to be found")
	}
	if report.Target.UniqueStops != report.Partition.StopsServedByOne+report.Partition.StopsServedByTwo {
		t.Fatalf("stop coverage %d must equal unique stops %d",
			report.Partition.StopsServedByOne+report.Partition.StopsServedByTwo, report.Target.UniqueStops)
	}
	if report.Partition.CallsVehicleOne+report.Partition.CallsVehicleTwo != report.Target.TotalCalls {
		t.Fatal("calls must be fully divided between the two vehicles")
	}
	if report.Partition.LongestVehicleRunSeconds == nil {
		t.Skip("occurrence has no measurable run span")
	}
	if *report.Partition.LongestVehicleRunSeconds > *report.Partition.FullRunSeconds {
		t.Fatal("a divided run must never be longer than the full run")
	}
	if report.Deadhead.Meters == nil || *report.Deadhead.Meters < 0 {
		t.Fatalf("expected a non-negative distance, got %v", report.Deadhead.Meters)
	}
	if len(report.Assumptions) == 0 {
		t.Fatal("assumptions must be reported with the numbers")
	}
}
