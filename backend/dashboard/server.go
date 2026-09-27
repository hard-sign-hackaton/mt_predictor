package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"mt_predictor/models"
)

// API обслуживает статический каталог, текущий снимок и SSE-поток карты.
type API struct {
	init                models.DashboardInit
	runtime             *Runtime
	enableMockScenarios bool
	incidentThreshold   float64
}

// NewAPI создаёт HTTP handler без запуска отдельного listener.
func NewAPI(
	init models.DashboardInit,
	runtime *Runtime,
	enableMockScenarios bool,
	incidentThreshold float64,
) http.Handler {
	if incidentThreshold <= 0 {
		incidentThreshold = 120
	}
	api := &API{
		init: init, runtime: runtime, enableMockScenarios: enableMockScenarios,
		incidentThreshold: incidentThreshold,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/map/init", api.handleInit)
	mux.HandleFunc("GET /api/v1/map/snapshot", api.handleSnapshot)
	mux.HandleFunc("GET /api/v1/dashboard/snapshot", api.handleDashboardSnapshot)
	mux.HandleFunc("GET /api/v1/map/events", api.handleEvents)
	mux.HandleFunc("GET /health", api.handleHealth)
	if enableMockScenarios {
		mux.HandleFunc("POST /api/v1/demo/scenarios", api.handleMockScenarios)
	}
	return withCORS(mux, enableMockScenarios)
}

func (a *API) handleInit(response http.ResponseWriter, _ *http.Request) {
	writeJSON(response, http.StatusOK, a.init)
}

func (a *API) handleSnapshot(response http.ResponseWriter, _ *http.Request) {
	writeJSON(response, http.StatusOK, a.runtime.Snapshot(time.Now()))
}

func (a *API) handleDashboardSnapshot(response http.ResponseWriter, _ *http.Request) {
	writeJSON(response, http.StatusOK, a.runtime.DashboardSnapshot(time.Now()))
}

func (a *API) handleHealth(response http.ResponseWriter, _ *http.Request) {
	snapshot := a.runtime.DashboardSnapshot(time.Now())
	writeJSON(response, http.StatusOK, map[string]any{
		"status": "ok", "catalogVersion": a.init.CatalogVersion,
		"streamVersion": snapshot.Version, "vehicles": len(snapshot.Vehicles),
		"predictions": len(snapshot.Predictions), "incidents": len(snapshot.Incidents),
	})
}

func (a *API) handleEvents(response http.ResponseWriter, request *http.Request) {
	flusher, ok := response.(http.Flusher)
	if !ok {
		http.Error(response, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	response.Header().Set("Content-Type", "text/event-stream")
	response.Header().Set("Cache-Control", "no-cache")
	response.Header().Set("Connection", "keep-alive")

	events, unsubscribe := a.runtime.Subscribe()
	defer unsubscribe()
	if err := writeSSE(response, "snapshot", a.runtime.DashboardSnapshot(time.Now())); err != nil {
		return
	}
	flusher.Flush()

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case event := <-events:
			if err := writeSSE(response, string(event.Type), event); err != nil {
				return
			}
			flusher.Flush()
		case <-heartbeat.C:
			if _, err := fmt.Fprint(response, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-request.Context().Done():
			return
		}
	}
}

func writeSSE(response http.ResponseWriter, event string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(response, "event: %s\ndata: %s\n\n", event, data)
	return err
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}

func withCORS(next http.Handler, allowMockIngest bool) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Access-Control-Allow-Origin", "*")
		methods := "GET, OPTIONS"
		if allowMockIngest {
			methods = "GET, POST, OPTIONS"
		}
		response.Header().Set("Access-Control-Allow-Methods", methods)
		response.Header().Set("Access-Control-Allow-Headers", "Content-Type, Last-Event-ID")
		if request.Method == http.MethodOptions {
			response.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(response, request)
	})
}
