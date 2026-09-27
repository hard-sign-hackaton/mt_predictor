package dashboard

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mt_predictor/models"
)

func TestMapEndpointsReturnOnlyMapContract(t *testing.T) {
	runtime := &Runtime{ttl: time.Minute, vehicles: make(map[uint32]models.VehicleState), subscribers: make(map[uint64]chan models.LiveEvent)}
	handler := NewAPI(models.DashboardInit{SchemaVersion: "1", CatalogVersion: "test"}, runtime, false, 120)

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
