package dashboard

import (
	"context"
	"testing"
	"time"

	"mt_predictor/catalog"
	"mt_predictor/internal/mlclient"
	"mt_predictor/models"
)

type recordingPredictor struct {
	telemetry []mlclient.TelemetryPoint
}

func (p *recordingPredictor) PredictBatch(_ context.Context, points []mlclient.PredictionPoint, telemetry []mlclient.TelemetryPoint) (map[string]float64, error) {
	p.telemetry = append([]mlclient.TelemetryPoint(nil), telemetry...)
	result := make(map[string]float64, len(points))
	for _, point := range points {
		result[point.SampleID] = 240
	}
	return result, nil
}

func TestSelectTargetUsesOpenTenAndClosedFifteenMinuteWindow(t *testing.T) {
	now := time.Date(2026, 1, 6, 10, 0, 0, 0, time.UTC)
	events := []catalog.ScheduleEvent{
		{ActionItemID: 10, PlannedAt: now.Add(10 * time.Minute)},
		{ActionItemID: 11, PlannedAt: now.Add(11 * time.Minute)},
		{ActionItemID: 15, PlannedAt: now.Add(15 * time.Minute)},
	}
	target, ok := selectTarget(events, now)
	if !ok || target.ActionItemID != 11 {
		t.Fatalf("ожидалась первая цель после T+10: %+v", target)
	}
	target, ok = selectTarget(events[2:], now)
	if !ok || target.ActionItemID != 15 {
		t.Fatalf("граница T+15 должна включаться: %+v", target)
	}
	if _, ok := selectTarget(events[:1], now); ok {
		t.Fatal("граница T+10 не должна включаться")
	}
}

func TestCurrentDelayAppearsAfterTwoPacketsOnNextSegment(t *testing.T) {
	processor := &Processor{}
	progress := &vehicleProgress{confirmedSegment: -1, candidateSegment: -1}
	planned := time.Date(2026, 1, 6, 10, 0, 0, 0, time.UTC)
	first := catalog.MatchResult{Status: catalog.MatchMatched, OccurrenceID: "run", SegmentIndex: 3, PreviousPlannedAt: planned}
	processor.updateDelay(progress, first, planned)
	if progress.currentDelay != nil {
		t.Fatal("первый match не должен придумывать задержку")
	}

	next := catalog.MatchResult{Status: catalog.MatchMatched, OccurrenceID: "run", SegmentIndex: 4, PreviousPlannedAt: planned.Add(time.Minute)}
	processor.updateDelay(progress, next, planned.Add(3*time.Minute))
	if progress.currentDelay != nil {
		t.Fatal("одного пакета нового сегмента недостаточно")
	}
	processor.updateDelay(progress, next, planned.Add(3*time.Minute+15*time.Second))
	if progress.currentDelay == nil || *progress.currentDelay != 120 {
		t.Fatalf("ожидалось cur_dev_s=120, получено %v", progress.currentDelay)
	}

	backward := catalog.MatchResult{Status: catalog.MatchMatched, OccurrenceID: "run", SegmentIndex: 2, PreviousPlannedAt: planned}
	processor.updateDelay(progress, backward, planned.Add(4*time.Minute))
	if *progress.currentDelay != 120 {
		t.Fatal("обратный скачок не должен менять подтверждённую задержку")
	}
}

func TestFlushNeverSendsTelemetryAfterPredictionTime(t *testing.T) {
	predictor := &recordingPredictor{}
	runtime := NewRuntime(catalog.NewMatcher(nil), time.Minute)
	processor := NewProcessor(nil, runtime, predictor, ProcessorOptions{IncidentThreshold: 120})
	forecast := time.Date(2026, 1, 6, 10, 0, 0, 0, time.UTC)
	processor.pending[7] = predictionCandidate{
		point:     mlclient.PredictionPoint{SampleID: "sample", TRID: 42, PredictionTime: forecast, TargetStopID: 9, TargetTime: forecast.Add(12 * time.Minute), CurrentDelay: 30},
		telemetry: []mlclient.TelemetryPoint{{TRID: 42, EventTime: forecast.Add(-time.Second)}, {TRID: 42, EventTime: forecast.Add(time.Second)}},
		unitID:    7, routePatternID: "pattern", targetStop: models.StopReference{ID: "stop"},
	}
	processor.flush(context.Background())
	if len(predictor.telemetry) != 1 || predictor.telemetry[0].EventTime.After(forecast) {
		t.Fatalf("в ML ушла будущая телеметрия: %+v", predictor.telemetry)
	}
	if len(runtime.DashboardSnapshot(time.Now()).Incidents) != 1 {
		t.Fatal("прогноз 240 секунд должен создать инцидент")
	}
}

func TestIncidentLifecycleUsesStrictThresholdAndStableID(t *testing.T) {
	runtime := NewRuntime(catalog.NewMatcher(nil), time.Minute)
	now := time.Date(2026, 1, 6, 10, 0, 0, 0, time.UTC)
	prediction := models.DelayPrediction{ID: "p1", UnitID: 7, TRID: 42, RoutePatternID: "pattern", TargetActionItemID: 9, TargetStop: models.StopReference{ID: "stop"}, PredictedDelaySeconds: 121}
	runtime.ApplyPrediction(prediction, 120, now)
	first := runtime.DashboardSnapshot(now).Incidents
	if len(first) != 1 || first[0].EventType != models.IncidentNew {
		t.Fatalf("ожидался новый инцидент: %+v", first)
	}
	id, createdAt := first[0].ID, first[0].CreatedAt

	prediction.ID = "p2"
	prediction.PredictedDelaySeconds = 180
	runtime.ApplyPrediction(prediction, 120, now.Add(time.Minute))
	updated := runtime.DashboardSnapshot(now).Incidents
	if len(updated) != 1 || updated[0].ID != id || updated[0].CreatedAt != createdAt || updated[0].EventType != models.IncidentUpdated {
		t.Fatalf("инцидент должен обновиться с тем же ID: %+v", updated)
	}

	prediction.PredictedDelaySeconds = 120
	runtime.ApplyPrediction(prediction, 120, now.Add(2*time.Minute))
	if len(runtime.DashboardSnapshot(now).Incidents) != 0 {
		t.Fatal("значение на пороге 120 должно закрыть инцидент")
	}
}
