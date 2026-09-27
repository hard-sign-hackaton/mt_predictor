package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mt_predictor/models"
)

func TestMapEndpointsReturnOnlyMapContract(t *testing.T) {
	runtime := &Runtime{ttl: time.Minute, vehicles: make(map[uint32]models.VehicleState), subscribers: make(map[uint64]chan models.LiveEvent)}
	handler := NewAPI(models.DashboardInit{SchemaVersion: "1", CatalogVersion: "test"}, runtime)

	for _, path := range []string{"/api/v1/map/init", "/api/v1/map/snapshot", "/api/v1/dashboard/snapshot", "/health"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("%s вернул %d", path, response.Code)
		}
		if contentType := response.Header().Get("Content-Type"); !strings.Contains(contentType, "application/json") {
			t.Fatalf("%s вернул неверный Content-Type: %s", path, contentType)
		}
	}
}

func TestIncidentActionsAreReasonSpecificAndStored(t *testing.T) {
	runtime := NewRuntime(nil, time.Minute)
	incident := models.Incident{ID: "0f0472bd-91b3-4dc2-9b3e-c7809febc66b", Status: models.IncidentActive, UnitID: 42, RoutePatternID: "route-42", ReasonCode: "door_hold_delay"}
	runtime.incidentArchive[incident.ID] = incident
	handler := NewAPI(models.DashboardInit{}, runtime)

	get := httptest.NewRecorder()
	handler.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/v1/incidents/"+incident.ID+"/actions", nil))
	if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), "reduce_stop_dwell") || strings.Contains(get.Body.String(), "request_reserve") {
		t.Fatalf("wrong actions for door incident: status=%d body=%s", get.Code, get.Body.String())
	}

	post := httptest.NewRecorder()
	handler.ServeHTTP(post, httptest.NewRequest(http.MethodPost, "/api/v1/incidents/"+incident.ID+"/actions", strings.NewReader(`{"actionCode":"reduce_stop_dwell"}`)))
	if post.Code != http.StatusCreated {
		t.Fatalf("action rejected: %d %s", post.Code, post.Body.String())
	}
	var saved models.OperatorAction
	if err := json.Unmarshal(post.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.UnitID != 42 || saved.RoutePatternID != "route-42" || saved.Recipient != "driver" || saved.Status != models.OperatorActionPending {
		t.Fatalf("incomplete action outbox record: %+v", saved)
	}

	history := httptest.NewRecorder()
	handler.ServeHTTP(history, httptest.NewRequest(http.MethodGet, "/api/v1/incidents/"+incident.ID+"/actions", nil))
	if !strings.Contains(history.Body.String(), saved.ID) {
		t.Fatalf("saved action missing from history: %s", history.Body.String())
	}
}
