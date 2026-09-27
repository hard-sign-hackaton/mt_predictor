package dashboard

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"mt_predictor/catalog"
	"mt_predictor/models"
)

func TestMockScenarioDatasetIsAcceptedAndDiagnosed(t *testing.T) {
	data, err := os.ReadFile("../data/mock_scenarios.json")
	if err != nil {
		t.Fatal(err)
	}
	var batch mockScenarioBatch
	if err := json.Unmarshal(data, &batch); err != nil {
		t.Fatal(err)
	}
	batch.ReplaySpeed = 0
	data, err = json.Marshal(batch)
	if err != nil {
		t.Fatal(err)
	}
	runtime := NewRuntime(catalog.NewMatcher(nil), time.Minute)
	handler := NewAPI(models.DashboardInit{SchemaVersion: "1"}, runtime, APIOptions{EnableMockScenarios: true, IncidentThreshold: 120})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/demo/scenarios", bytes.NewReader(data))
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("dataset rejected: status=%d body=%s", response.Code, response.Body.String())
	}
	snapshot := runtime.DashboardSnapshot(time.Now())
	if len(snapshot.Predictions) != 5 {
		t.Fatalf("want one prediction for each mock vehicle, got %d", len(snapshot.Predictions))
	}
	if len(snapshot.Incidents) != 5 {
		t.Fatalf("want 5 active incidents, got %d", len(snapshot.Incidents))
	}
	if got := len(runtime.Snapshot(time.Now()).Vehicles); got != 5 {
		t.Fatalf("want 5 materialized mock vehicles, got %d", got)
	}
	units := make(map[uint32]struct{}, len(snapshot.Predictions))
	reasons := make(map[string]struct{}, len(snapshot.Predictions))
	for _, prediction := range snapshot.Predictions {
		units[prediction.UnitID] = struct{}{}
		reasons[prediction.ReasonCode] = struct{}{}
		if prediction.ReasonCode == "" || prediction.Reason == nil || prediction.ScenarioID == "" {
			t.Errorf("diagnosis missing from prediction %q: %+v", prediction.ID, prediction)
		}
		if prediction.PredictedDelaySeconds < -900 || prediction.PredictedDelaySeconds > 900 {
			t.Errorf("test delay escaped bounded range: %v", prediction.PredictedDelaySeconds)
		}
	}
	if len(units) != 5 || len(reasons) != 5 {
		t.Fatalf("mock dataset must cover 5 vehicles and 5 reasons: units=%d reasons=%d", len(units), len(reasons))
	}
	for _, incident := range snapshot.Incidents {
		if incident.Reason == nil || incident.ReasonCode == "" || incident.ScenarioID == "" {
			t.Errorf("diagnosis missing from incident %q: %+v", incident.ID, incident)
		}
	}
}

func TestMockReplayRebasesScenarioTimes(t *testing.T) {
	startedAt := time.Date(2026, 9, 27, 18, 35, 40, 0, time.UTC)
	sourcePrediction := time.Date(2026, 1, 6, 6, 29, 0, 0, time.UTC)
	sourceTarget := sourcePrediction.Add(12 * time.Minute)

	dueAt := startedAt.Add(scaleDuration(time.Minute, 25))
	targetAt := dueAt.Add(sourceTarget.Sub(sourcePrediction))
	if got, want := dueAt.Sub(startedAt), 2400*time.Millisecond; got != want {
		t.Fatalf("wrong replay offset: got %s want %s", got, want)
	}
	if got, want := targetAt.Sub(dueAt), 12*time.Minute; got != want {
		t.Fatalf("wrong rebased planning horizon: got %s want %s", got, want)
	}
	if targetAt.Year() != startedAt.Year() || targetAt.Month() != startedAt.Month() || targetAt.Day() != startedAt.Day() {
		t.Fatalf("planned arrival was not rebased to replay date: %s", targetAt)
	}
}

func TestMockScenarioIngestIsDisabledByDefault(t *testing.T) {
	runtime := NewRuntime(catalog.NewMatcher(nil), time.Minute)
	handler := NewAPI(models.DashboardInit{}, runtime)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/demo/scenarios", bytes.NewBufferString(`{"scenarios":[]}`))
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("disabled mock endpoint returned %d, want 404", response.Code)
	}
}

func TestMockScenarioBatchValidationDoesNotPartiallyApply(t *testing.T) {
	body := `{
		"scenarios": [{
			"scenarioId": "bad-label",
			"unitId": 990000101,
			"trId": 99000101,
			"routePatternId": "mock-route",
			"targetActionItemId": 9900101,
			"targetStop": {"id":"mock-stop","address":"Test"},
			"predictionTime":"2026-06-10T08:00:00Z",
			"currentPlannedAt":"2026-06-10T07:59:00Z",
			"targetPlannedAt":"2026-06-10T08:12:00Z",
			"predictedDelaySeconds":300,
			"expectedReasonCode":"traffic_slowdown",
			"telemetryHistory":[{"secondsBefore":60,"speedKmh":10,"headingDegrees":0},{"secondsBefore":0,"speedKmh":8,"headingDegrees":0}],
			"evidence":{"door_open_duration_s":140}
		}]
	}`
	runtime := NewRuntime(catalog.NewMatcher(nil), time.Minute)
	handler := NewAPI(models.DashboardInit{}, runtime, APIOptions{EnableMockScenarios: true, IncidentThreshold: 120})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/demo/scenarios", bytes.NewBufferString(body))
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("wrong expected label status=%d body=%s", response.Code, response.Body.String())
	}
	if got := len(runtime.DashboardSnapshot(time.Now()).Predictions); got != 0 {
		t.Fatalf("invalid batch was partially applied: %d predictions", got)
	}
}

func TestMockDiagnosisUsesEvidenceRatherThanExpectedLabel(t *testing.T) {
	input := mockScenario{
		ScenarioID: "door-test", UnitID: 990000101, TRID: 99000101,
		RoutePatternID: "mock-route", TargetActionItemID: 9900101,
		TargetStop:            models.StopReference{ID: "stop"},
		PredictionTime:        time.Date(2026, 6, 10, 8, 0, 0, 0, time.UTC),
		CurrentPlannedAt:      time.Date(2026, 6, 10, 7, 59, 0, 0, time.UTC),
		TargetPlannedAt:       time.Date(2026, 6, 10, 8, 12, 0, 0, time.UTC),
		PredictedDelaySeconds: 300, ExpectedReasonCode: "door_hold_delay",
		TelemetryHistory: []mockTelemetrySample{
			{SecondsBefore: 180, SpeedKmh: 0, DoorOpen: true},
			{SecondsBefore: 90, SpeedKmh: 0, DoorOpen: true},
			{SecondsBefore: 0, SpeedKmh: 0, DoorOpen: true},
		},
	}
	encoded, err := json.Marshal(mockScenarioBatch{Scenarios: []mockScenario{input}})
	if err != nil {
		t.Fatal(err)
	}
	runtime := NewRuntime(catalog.NewMatcher(nil), time.Minute)
	handler := NewAPI(models.DashboardInit{}, runtime, APIOptions{EnableMockScenarios: true, IncidentThreshold: 120})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/demo/scenarios", bytes.NewReader(encoded))
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("scenario rejected: status=%d body=%s", response.Code, response.Body.String())
	}
	var result struct {
		Results []struct {
			ReasonCode string `json:"reasonCode"`
		} `json:"results"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Results) != 1 || result.Results[0].ReasonCode != "door_hold_delay" {
		t.Fatalf("rules did not diagnose door hold: %+v", result)
	}
}

func testMockTelemetry() []mockTelemetrySample {
	return []mockTelemetrySample{
		{SecondsBefore: 60, SpeedKmh: 20, HeadingDegrees: 90},
		{SecondsBefore: 0, SpeedKmh: 10, HeadingDegrees: 90},
	}
}

func TestMockScenarioSendsFullChronologicalHistoryToML(t *testing.T) {
	predictor := &recordingPredictor{}
	now := time.Date(2026, 6, 10, 8, 0, 0, 0, time.UTC)
	scenario := mockScenario{
		ScenarioID: "history-test", UnitID: 99, TRID: 101,
		RoutePatternID: "mock-route", TargetActionItemID: 1001,
		TargetStop:     models.StopReference{ID: "mock-stop"},
		PredictionTime: now, CurrentPlannedAt: now.Add(-time.Minute),
		TargetPlannedAt: now.Add(12 * time.Minute), PredictedDelaySeconds: 100,
		TelemetryHistory: []mockTelemetrySample{
			{SecondsBefore: 0, SpeedKmh: 5, HeadingDegrees: 90},
			{SecondsBefore: 120, SpeedKmh: 25, HeadingDegrees: 90},
			{SecondsBefore: 60, SpeedKmh: 15, HeadingDegrees: 90},
		},
		Evidence: map[string]float64{},
	}
	encoded, err := json.Marshal(mockScenarioBatch{BindToLiveVehicle: true, Scenarios: []mockScenario{scenario}})
	if err != nil {
		t.Fatal(err)
	}
	runtime := NewRuntime(catalog.NewMatcher(nil), time.Minute)
	handler := NewAPI(models.DashboardInit{}, runtime, APIOptions{
		EnableMockScenarios: true, IncidentThreshold: 120, Predictor: predictor, PredictionTimeout: time.Second,
	})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/demo/scenarios", bytes.NewReader(encoded))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("scenario rejected: status=%d body=%s", response.Code, response.Body.String())
	}
	if len(predictor.telemetry) != 3 {
		t.Fatalf("ML received %d telemetry points, want 3", len(predictor.telemetry))
	}
	for index := 1; index < len(predictor.telemetry); index++ {
		if predictor.telemetry[index].EventTime.Before(predictor.telemetry[index-1].EventTime) {
			t.Fatalf("telemetry is not chronological: %+v", predictor.telemetry)
		}
	}
	if got := *predictor.telemetry[2].Speed; got != 5 {
		t.Fatalf("latest telemetry speed=%v, want 5", got)
	}
}

func TestMockScenarioRejectsFortyMinuteDelayArtifact(t *testing.T) {
	scenario := mockScenario{
		ScenarioID: "bounded-test", UnitID: 990000101, TRID: 99000101,
		RoutePatternID: "mock-route", TargetActionItemID: 9900101,
		TargetStop:            models.StopReference{ID: "mock-stop"},
		PredictionTime:        time.Date(2026, 6, 10, 8, 0, 0, 0, time.UTC),
		CurrentPlannedAt:      time.Date(2026, 6, 10, 7, 59, 0, 0, time.UTC),
		TargetPlannedAt:       time.Date(2026, 6, 10, 8, 12, 0, 0, time.UTC),
		PredictedDelaySeconds: 40 * 60,
	}
	if err := scenario.validate(); err == nil {
		t.Fatal("40-minute delay artifact was accepted by the bounded test contract")
	}
}

func TestMockDiagnosisRulesAndPriority(t *testing.T) {
	delay := 180.0
	tests := []struct {
		name     string
		evidence map[string]float64
		current  *float64
		want     string
	}{
		{"gps age wins over all", map[string]float64{"last_gps_age_s": 120, "route_deviation_m": 200, "door_open_duration_s": 140}, &delay, "poor_gps_quality"},
		{"few gps points", map[string]float64{"valid_gps_points_5m": 1}, nil, "poor_gps_quality"},
		{"route deviation wins over doors", map[string]float64{"route_deviation_m": 100, "door_open_duration_s": 140}, nil, "route_deviation"},
		{"door hold", map[string]float64{"door_open_duration_s": 90}, nil, "door_hold_delay"},
		{"traffic wins over stop dwell", map[string]float64{"speed_mean_5m_kmh": 14.9, "congestion_index": .7, "stationary_duration_s": 150}, nil, "traffic_slowdown"},
		{"stop dwell", map[string]float64{"stationary_duration_s": 120}, nil, "stop_dwell"},
		{"schedule slippage", map[string]float64{}, &delay, "schedule_slippage"},
		{"insufficient evidence", map[string]float64{"speed_mean_5m_kmh": 14.9}, nil, "insufficient_evidence"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := diagnoseMockEvidence(test.evidence, test.current).code; got != test.want {
				t.Fatalf("got %q, want %q", got, test.want)
			}
		})
	}
}
