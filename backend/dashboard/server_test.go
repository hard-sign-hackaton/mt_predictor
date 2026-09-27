package dashboard

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"mt_predictor/catalog"
	"mt_predictor/models"
)

func testRuntime() *Runtime {
	return &Runtime{
		ttl: time.Minute, vehicles: make(map[uint32]models.VehicleState),
		subscribers: make(map[uint64]chan models.LiveEvent),
	}
}

func TestMapEndpointsReturnOnlyMapContract(t *testing.T) {
	handler := NewAPI(models.DashboardInit{SchemaVersion: "1", CatalogVersion: "test"}, testRuntime(), nil)

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

// whatIfHandler поднимает handler на реальном сгенерированном каталоге и
// возвращает самый длинный рейс вместе с unit_id одного из ТС без расписания.
func whatIfHandler(t *testing.T) (http.Handler, string, uint32) {
	t.Helper()
	routeCatalog, err := catalog.Load("../data/generated/route_catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	occurrenceID, mostCalls := "", -1
	for _, assignment := range routeCatalog.Assignments {
		if len(assignment.Events) > mostCalls {
			occurrenceID, mostCalls = assignment.OccurrenceID, len(assignment.Events)
		}
	}
	if occurrenceID == "" || mostCalls == 0 {
		t.Skip("no occurrence in generated catalog")
	}
	var candidate uint32
	for _, binding := range routeCatalog.VehicleBindings {
		if !binding.HasSchedule && !binding.Synthetic {
			candidate = binding.UnitID
			break
		}
	}
	if candidate == 0 {
		t.Skip("no vehicle without schedule in generated catalog")
	}
	runtime := testRuntime()
	handler := NewAPI(routeCatalog.DashboardInit("test", models.RiskThresholds{}), runtime, routeCatalog)
	return handler, occurrenceID, candidate
}

func decodeWhatIf(t *testing.T, response *httptest.ResponseRecorder) models.WhatIfReport {
	t.Helper()
	var report models.WhatIfReport
	if err := json.Unmarshal(response.Body.Bytes(), &report); err != nil {
		t.Fatalf("не удалось разобрать ответ: %v, тело: %s", err, response.Body.String())
	}
	return report
}

func TestWhatIfRejectsMalformedQuery(t *testing.T) {
	handler, occurrenceID, candidate := whatIfHandler(t)
	cases := map[string]struct{ path, contains string }{
		"без unitId":         {path: "/api/v1/whatif?occurrenceID=x", contains: "unitId"},
		"нечисловой unitId":  {path: "/api/v1/whatif?unitId=abc&occurrenceId=x", contains: "unitId"},
		"без occurrenceId":   {path: "/api/v1/whatif?unitId=1", contains: "occurrenceId"},
		"плохой joinIndex":   {path: "/api/v1/whatif?unitId=1&occurrenceId=x&joinCallIndex=a", contains: "joinCallIndex"},
		"плохая скорость":    {path: "/api/v1/whatif?unitId=1&occurrenceId=x&emptySpeedKmh=fast", contains: "emptySpeedKmh"},
		"плохое topStops":    {path: "/api/v1/whatif?unitId=1&occurrenceId=x&topStops=many", contains: "topStops"},
		"пустой unitId":      {path: "/api/v1/whatif?unitId=&occurrenceId=" + occurrenceID, contains: "unitId"},
		"лишние пробелы":     {path: "/api/v1/whatif?unitId=%20&occurrenceId=x", contains: "unitId"},
		"отрицательный unit": {path: "/api/v1/whatif?unitId=-1&occurrenceId=x", contains: "unitId"},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, testCase.path, nil))
			if response.Code != http.StatusBadRequest {
				t.Fatalf("ожидался 400, получен %d", response.Code)
			}
			if !strings.Contains(response.Body.String(), testCase.contains) {
				t.Fatalf("ожидалось сообщение про %s, получено: %s", testCase.contains, response.Body.String())
			}
		})
	}
	if candidate == 0 {
		t.Fatal("expected a candidate unit")
	}
}

func TestWhatIfUnknownOccurrenceReturns404(t *testing.T) {
	handler, _, candidate := whatIfHandler(t)
	response := httptest.NewRecorder()
	path := "/api/v1/whatif?unitId=" + itoa(candidate) + "&occurrenceId=occ_missing"
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("ожидался 404, получен %d: %s", response.Code, response.Body.String())
	}
}

func TestWhatIfVehicleWithoutTelemetryIsNotAnError(t *testing.T) {
	handler, occurrenceID, candidate := whatIfHandler(t)
	response := httptest.NewRecorder()
	path := "/api/v1/whatif?unitId=" + itoa(candidate) + "&occurrenceId=" + occurrenceID
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
	if response.Code != http.StatusOK {
		t.Fatalf("ожидался 200, получен %d: %s", response.Code, response.Body.String())
	}
	report := decodeWhatIf(t, response)
	if report.Deadhead.Verdict == nil || *report.Deadhead.Verdict != models.WhatIfUnknownVehicle {
		t.Fatalf("ожидался verdict %s, получен %v", models.WhatIfUnknownVehicle, report.Deadhead.Verdict)
	}
	if report.Deadhead.Meters != nil {
		t.Fatal("отсутствие телеметрии не должно давать расстояние")
	}
	if report.Deadhead.UnavailableReason != models.WhatIfReasonUnknownVehicle {
		t.Fatalf("ожидалась причина %s, получена %q", models.WhatIfReasonUnknownVehicle, report.Deadhead.UnavailableReason)
	}
}

func TestWhatIfComputesDeadheadForLiveVehicle(t *testing.T) {
	handler, occurrenceID, candidate := whatIfHandler(t)
	routeCatalog, err := catalog.Load("../data/generated/route_catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	assignment, ok := routeCatalog.Assignment(occurrenceID)
	if !ok {
		t.Fatal("occurrence must exist in catalog")
	}
	// Фидер в режиме source отдаёт event_time из исходных данных, поэтому позиция
	// кандидата наблюдается в той же шкале времени, что и planned_at рейса.
	observedAt := assignment.ValidFrom
	runtime := testRuntime()
	runtime.vehicles[candidate] = models.VehicleState{
		UnitID: candidate, Position: &models.GeoPoint{Lon: 37.617, Lat: 55.755},
		Freshness: models.TelemetryLive, MatchStatus: models.MatchNoSchedule,
		EventTime: observedAt, ReceivedAt: observedAt,
	}
	handler = NewAPI(routeCatalog.DashboardInit("test", models.RiskThresholds{}), runtime, routeCatalog)

	response := httptest.NewRecorder()
	path := "/api/v1/whatif?unitId=" + itoa(candidate) + "&occurrenceId=" + occurrenceID + "&joinCallIndex=0"
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
	if response.Code != http.StatusOK {
		t.Fatalf("ожидался 200, получен %d: %s", response.Code, response.Body.String())
	}
	report := decodeWhatIf(t, response)
	if report.Deadhead.Meters == nil || *report.Deadhead.Meters <= 0 {
		t.Fatalf("ожидалось положительное расстояние, получено %v", report.Deadhead.Meters)
	}
	if report.Deadhead.Seconds == nil || *report.Deadhead.Seconds <= 0 {
		t.Fatalf("ожидалось положительное время перегона, получено %v", report.Deadhead.Seconds)
	}
	if report.Deadhead.AssumedEmptySpeedKmh != catalog.WhatIfDefaultEmptySpeedKmh {
		t.Fatalf("ожидалась скорость по умолчанию, получена %v", report.Deadhead.AssumedEmptySpeedKmh)
	}
	// Запас обязан считаться и быть правдоподобным: иначе стенные часы сервера,
	// отстоящие от расписания на месяцы, снова дадут −6329 ч.
	if report.Deadhead.SlackSeconds == nil {
		t.Fatalf("ожидался рассчитанный запас, получен вердикт %v и причина %q",
			*report.Deadhead.Verdict, report.Deadhead.UnavailableReason)
	}
	if slack := *report.Deadhead.SlackSeconds; math.Abs(slack) > 24*3600 {
		t.Fatalf("ожидался правдоподобный запас, получено %.0f часов", slack/3600)
	}
	if report.Deadhead.AvailableFrom == nil || !report.Deadhead.AvailableFrom.Equal(observedAt) {
		t.Fatalf("опорное время должно быть моментом фиксации позиции, получено %v", report.Deadhead.AvailableFrom)
	}
	if report.ScheduleReliefSeconds != nil {
		t.Fatal("сокращение задержки по графику не должно вычисляться")
	}
	if report.UnavailableReason != models.WhatIfReasonNoCapacityModel {
		t.Fatalf("ожидалась причина %s, получена %q", models.WhatIfReasonNoCapacityModel, report.UnavailableReason)
	}
}

// TestWhatIfRefusesPositionOutsideRunHorizon: если позиция наблюдалась уже после
// границы разбиения, сценарий неприменим и запас не вычисляется. Отказ должен
// остаться честным, а не превращаться в число в тысячи часов.
func TestWhatIfRefusesPositionOutsideRunHorizon(t *testing.T) {
	_, occurrenceID, candidate := whatIfHandler(t)
	routeCatalog, err := catalog.Load("../data/generated/route_catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	assignment, ok := routeCatalog.Assignment(occurrenceID)
	if !ok {
		t.Fatal("occurrence must exist in catalog")
	}
	runtime := testRuntime()
	// Позиция наблюдалась на два дня позже конца рейса.
	late := assignment.ValidTo.Add(48 * time.Hour)
	runtime.vehicles[candidate] = models.VehicleState{
		UnitID: candidate, Position: &models.GeoPoint{Lon: 37.617, Lat: 55.755},
		Freshness: models.TelemetryLive, MatchStatus: models.MatchNoSchedule,
		EventTime: late, ReceivedAt: late,
	}
	handler := NewAPI(routeCatalog.DashboardInit("test", models.RiskThresholds{}), runtime, routeCatalog)

	response := httptest.NewRecorder()
	path := "/api/v1/whatif?unitId=" + itoa(candidate) + "&occurrenceId=" + occurrenceID + "&joinCallIndex=0"
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
	if response.Code != http.StatusOK {
		t.Fatalf("ожидался 200, получен %d: %s", response.Code, response.Body.String())
	}
	report := decodeWhatIf(t, response)
	if report.Deadhead.SlackSeconds != nil {
		t.Fatalf("запас не должен вычисляться вне горизонта, получено %.0f часов", *report.Deadhead.SlackSeconds/3600)
	}
	if report.Deadhead.Verdict == nil || *report.Deadhead.Verdict != models.WhatIfRunInPast {
		t.Fatalf("ожидался verdict %s, получен %v", models.WhatIfRunInPast, report.Deadhead.Verdict)
	}
	// Расстояние остаётся фактом и не зависит от момента наблюдения.
	if report.Deadhead.Meters == nil {
		t.Fatal("ожидалось расстояние до границы разбиения")
	}
}

func TestWhatIfDoesNotMutateRuntime(t *testing.T) {
	handler, occurrenceID, candidate := whatIfHandler(t)
	runtime := testRuntime()
	runtime.vehicles[candidate] = models.VehicleState{
		UnitID: candidate, Position: &models.GeoPoint{Lon: 37.617, Lat: 55.755},
		Freshness: models.TelemetryLive, EventTime: time.Now(), ReceivedAt: time.Now(),
	}
	routeCatalog, err := catalog.Load("../data/generated/route_catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	handler = NewAPI(routeCatalog.DashboardInit("test", models.RiskThresholds{}), runtime, routeCatalog)
	path := "/api/v1/whatif?unitId=" + itoa(candidate) + "&occurrenceId=" + occurrenceID
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
	snapshot := runtime.DashboardSnapshot(time.Now())
	if len(snapshot.Predictions) != 0 || len(snapshot.Incidents) != 0 || snapshot.Version != 0 {
		t.Fatalf("What-if не должен менять runtime: version=%d predictions=%d incidents=%d",
			snapshot.Version, len(snapshot.Predictions), len(snapshot.Incidents))
	}
}

func TestWhatIfRespectsRequestedMode(t *testing.T) {
	handler, occurrenceID, candidate := whatIfHandler(t)
	for _, mode := range []models.WhatIfMode{models.WhatIfModeRelieve, models.WhatIfModeDuplicate} {
		response := httptest.NewRecorder()
		path := "/api/v1/whatif?unitId=" + itoa(candidate) + "&occurrenceId=" + occurrenceID + "&mode=" + string(mode)
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("ожидался 200, получен %d: %s", response.Code, response.Body.String())
		}
		if report := decodeWhatIf(t, response); report.Mode != mode {
			t.Fatalf("ожидался режим %s, получен %s", mode, report.Mode)
		}
	}
}

func TestWhatIfIsReadOnlyMethod(t *testing.T) {
	handler, _, _ := whatIfHandler(t)
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(method, "/api/v1/whatif?unitId=1&occurrenceId=x", nil))
		if response.Code == http.StatusOK {
			t.Fatalf("%s не должен обрабатываться What-if", method)
		}
	}
}

func itoa(value uint32) string {
	return strconv.FormatUint(uint64(value), 10)
}
