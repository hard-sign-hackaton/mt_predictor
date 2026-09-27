package models

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestDelayPredictionOmitsUnavailableMLMetadata(t *testing.T) {
	prediction := DelayPrediction{
		ID: "sample-1", UnitID: 10, TRID: 20, RoutePatternID: "pattern-1",
		TargetActionItemID: 30, TargetStop: StopReference{ID: "stop-1"},
		PredictionTime:        time.Date(2026, 1, 6, 10, 0, 0, 0, time.UTC),
		TargetPlannedAt:       time.Date(2026, 1, 6, 10, 15, 0, 0, time.UTC),
		PredictedDelaySeconds: 420,
	}
	data, err := json.Marshal(prediction)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"confidence", "reason", "modelVersion"} {
		if strings.Contains(string(data), forbidden) {
			t.Fatalf("unavailable field %q was serialized: %s", forbidden, data)
		}
	}
}

func TestMinimalIncidentContainsNoOperatorWorkflow(t *testing.T) {
	incident := Incident{ID: "incident-1", EventType: IncidentNew, UnitID: 10, PredictedDelaySeconds: 420}
	data, err := json.Marshal(incident)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"owner", "assignee", "comment", "action", "whatIf", "risk"} {
		if strings.Contains(string(data), forbidden) {
			t.Fatalf("out-of-scope field %q was serialized: %s", forbidden, data)
		}
	}
}
