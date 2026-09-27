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
	runtime := NewRuntime(catalog.NewMatcher(nil), time.Minute)
	handler := NewAPI(models.DashboardInit{SchemaVersion: "1"}, runtime, true, 120)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/demo/scenarios", bytes.NewReader(data))
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("dataset rejected: status=%d body=%s", response.Code, response.Body.String())
	}
	snapshot := runtime.DashboardSnapshot(time.Now())
	if len(snapshot.Predictions) != 7 {
		t.Fatalf("want 7 predictions, got %d", len(snapshot.Predictions))
	}
	if len(snapshot.Incidents) != 6 {
		t.Fatalf("want 6 active incidents, got %d", len(snapshot.Incidents))
	}
	for _, prediction := range snapshot.Predictions {
		if prediction.ReasonCode == "" || prediction.Reason == nil || prediction.ScenarioID == "" {
			t.Errorf("diagnosis missing from prediction %q: %+v", prediction.ID, prediction)
		}
		if prediction.PredictedDelaySeconds < -900 || prediction.PredictedDelaySeconds > 900 {
			t.Errorf("test delay escaped bounded range: %v", prediction.PredictedDelaySeconds)
		}
	}
	for _, incident := range snapshot.Incidents {
		if incident.Reason == nil || incident.ReasonCode == "" || incident.ScenarioID == "" {
			t.Errorf("diagnosis missing from incident %q: %+v", incident.ID, incident)
		}
	}
}

func TestMockScenarioIngestIsDisabledByDefault(t *testing.T) {
	runtime := NewRuntime(catalog.NewMatcher(nil), time.Minute)
	handler := NewAPI(models.DashboardInit{}, runtime, false, 120)
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
			"targetPlannedAt":"2026-06-10T08:12:00Z",
			"predictedDelaySeconds":300,
			"expectedReasonCode":"traffic_slowdown",
			"evidence":{"door_open_duration_s":140}
		}]
	}`
	runtime := NewRuntime(catalog.NewMatcher(nil), time.Minute)
	handler := NewAPI(models.DashboardInit{}, runtime, true, 120)
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
		TargetStop: models.StopReference{ID: "stop"},
		PredictionTime: time.Date(2026, 6, 10, 8, 0, 0, 0, time.UTC),
		TargetPlannedAt: time.Date(2026, 6, 10, 8, 12, 0, 0, time.UTC),
		PredictedDelaySeconds: 300, ExpectedReasonCode: "door_hold_delay",
		Evidence: map[string]float64{"door_open_duration_s": 142},
	}
	encoded, err := json.Marshal(mockScenarioBatch{Scenarios: []mockScenario{input}})
	if err != nil {
		t.Fatal(err)
	}
	runtime := NewRuntime(catalog.NewMatcher(nil), time.Minute)
	handler := NewAPI(models.DashboardInit{}, runtime, true, 120)
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

func TestMockScenarioRejectsFortyMinuteDelayArtifact(t *testing.T) {
	scenario := mockScenario{
		ScenarioID: "bounded-test", UnitID: 990000101, TRID: 99000101,
		RoutePatternID: "mock-route", TargetActionItemID: 9900101,
		TargetStop: models.StopReference{ID: "mock-stop"},
		PredictionTime: time.Date(2026, 6, 10, 8, 0, 0, 0, time.UTC),
		TargetPlannedAt: time.Date(2026, 6, 10, 8, 12, 0, 0, time.UTC),
		PredictedDelaySeconds: 40 * 60,
	}
	if err := scenario.validate(); err == nil {
		t.Fatal("40-minute delay artifact was accepted by the bounded test contract")
	}
}
