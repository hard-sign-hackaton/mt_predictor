package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"mt_predictor/catalog"
	"mt_predictor/models"
)

// API обслуживает статический каталог, текущий снимок, SSE-поток карты и
// read-only сценарии What-if.
type API struct {
	init    models.DashboardInit
	runtime *Runtime
	catalog *catalog.Catalog
}

// NewAPI создаёт HTTP handler без запуска отдельного listener.
func NewAPI(init models.DashboardInit, runtime *Runtime, routeCatalog *catalog.Catalog) http.Handler {
	api := &API{init: init, runtime: runtime, catalog: routeCatalog}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/map/init", api.handleInit)
	mux.HandleFunc("GET /api/v1/map/snapshot", api.handleSnapshot)
	mux.HandleFunc("GET /api/v1/dashboard/snapshot", api.handleDashboardSnapshot)
	mux.HandleFunc("GET /api/v1/map/events", api.handleEvents)
	mux.HandleFunc("GET /api/v1/whatif", api.handleWhatIf)
	mux.HandleFunc("GET /health", api.handleHealth)
	return withCORS(mux)
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

// handleWhatIf отвечает на сценарий «выпуск дополнительного ТС». Обработчик
// ничего не меняет в runtime и не порождает событий: расчёт выполняется на
// копии снимка ТС и статическом каталоге.
//
// Отсутствие ТС в потоке телеметрии возвращается как 200 с verdict
// unknown_vehicle, а не как 404: ещё не появившееся в эфире ТС неотличимо от
// опечатки в идентификаторе, и frontend должен показать разные состояния.
// HTTP 404 остаётся только для неизвестного рейса, которого нет в каталоге.
func (a *API) handleWhatIf(response http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	rawUnitID := query.Get("unitId")
	unitID, err := strconv.ParseUint(rawUnitID, 10, 32)
	if rawUnitID == "" || err != nil {
		http.Error(response, "unitId must be an unsigned integer", http.StatusBadRequest)
		return
	}
	occurrenceID := query.Get("occurrenceId")
	if occurrenceID == "" {
		http.Error(response, "occurrenceId is required", http.StatusBadRequest)
		return
	}
	joinCallIndex, err := whatIfQueryInt(query.Get("joinCallIndex"), 0)
	if err != nil {
		http.Error(response, "joinCallIndex must be an integer", http.StatusBadRequest)
		return
	}
	emptySpeedKmh, err := whatIfQueryFloat(query.Get("emptySpeedKmh"), catalog.WhatIfDefaultEmptySpeedKmh)
	if err != nil {
		http.Error(response, "emptySpeedKmh must be a number", http.StatusBadRequest)
		return
	}
	topStops, err := whatIfQueryInt(query.Get("topStops"), catalog.WhatIfDefaultTopStops)
	if err != nil {
		http.Error(response, "topStops must be an integer", http.StatusBadRequest)
		return
	}

	vehicle, known := a.runtime.Vehicle(uint32(unitID))
	candidate := a.catalog.WhatIfCandidateState(uint32(unitID), vehicle, known)
	report, found := a.catalog.WhatIf(catalog.WhatIfRequest{
		UnitID: uint32(unitID), OccurrenceID: occurrenceID, JoinCallIndex: joinCallIndex,
		Mode: models.WhatIfMode(query.Get("mode")), EmptySpeedKmh: emptySpeedKmh, TopStops: topStops,
	}, candidate, time.Now())
	if !found {
		http.Error(response, "occurrence not found", http.StatusNotFound)
		return
	}
	writeJSON(response, http.StatusOK, report)
}

func whatIfQueryInt(raw string, fallback int) (int, error) {
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, err
	}
	return value, nil
}

func whatIfQueryFloat(raw string, fallback float64) (float64, error) {
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, err
	}
	return value, nil
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

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Access-Control-Allow-Origin", "*")
		response.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		response.Header().Set("Access-Control-Allow-Headers", "Content-Type, Last-Event-ID")
		if request.Method == http.MethodOptions {
			response.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(response, request)
	})
}
