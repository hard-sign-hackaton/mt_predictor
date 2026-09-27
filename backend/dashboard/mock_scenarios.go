package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"mt_predictor/internal/mlclient"
	"mt_predictor/models"
)

const (
	maxMockScenarioBytes = 1 << 20
	maxMockScenarioCount = 32
)

type mockScenarioBatch struct {
	ReplaySpeed       float64        `json:"replaySpeed,omitempty"`
	BindToLiveVehicle bool           `json:"bindToLiveVehicle,omitempty"`
	Scenarios         []mockScenario `json:"scenarios"`
}

type preparedMockScenario struct {
	input     mockScenario
	diagnosis mockDiagnosis
}

type mockScenario struct {
	ScenarioID            string                `json:"scenarioId"`
	UnitID                uint32                `json:"unitId"`
	TRID                  int64                 `json:"trId"`
	RoutePatternID        string                `json:"routePatternId"`
	TargetActionItemID    int64                 `json:"targetActionItemId"`
	TargetStop            models.StopReference  `json:"targetStop"`
	PredictionTime        time.Time             `json:"predictionTime"`
	CurrentPlannedAt      time.Time             `json:"currentPlannedAt"`
	TargetPlannedAt       time.Time             `json:"targetPlannedAt"`
	CurrentDelaySeconds   *float64              `json:"-"`
	PredictedDelaySeconds float64               `json:"predictedDelaySeconds"`
	ExpectedReasonCode    string                `json:"expectedReasonCode,omitempty"`
	Evidence              map[string]float64    `json:"evidence"`
	TelemetryHistory      []mockTelemetrySample `json:"telemetryHistory"`
}

// mockTelemetrySample описывает реальную входную последовательность ML, а не
// готовый прогноз. Координаты восстанавливаются назад от текущего положения ТС
// по скорости и курсу, поэтому вся история согласована по времени и движению.
type mockTelemetrySample struct {
	SecondsBefore        int      `json:"secondsBefore"`
	SpeedKmh             float64  `json:"speedKmh"`
	HeadingDegrees       float64  `json:"headingDegrees"`
	LocationValid        *bool    `json:"locationValid,omitempty"`
	DoorOpen             bool     `json:"doorOpen,omitempty"`
	CongestionIndex      *float64 `json:"congestionIndex,omitempty"`
	RouteDeviationMeters *float64 `json:"routeDeviationMeters,omitempty"`
}

type mockDiagnosis struct {
	code    string
	message string
}

func (a *API) handleMockScenarios(response http.ResponseWriter, request *http.Request) {
	request.Body = http.MaxBytesReader(response, request.Body, maxMockScenarioBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()

	var batch mockScenarioBatch
	if err := decoder.Decode(&batch); err != nil {
		writeJSONError(response, http.StatusBadRequest, err.Error())
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeJSONError(response, http.StatusBadRequest, "ожидался один JSON-объект")
		return
	}
	if len(batch.Scenarios) == 0 || len(batch.Scenarios) > maxMockScenarioCount {
		writeJSONError(response, http.StatusBadRequest,
			fmt.Sprintf("число сценариев должно быть от 1 до %d", maxMockScenarioCount))
		return
	}
	if batch.ReplaySpeed < 0 || !finite(batch.ReplaySpeed) || batch.ReplaySpeed > 1000 {
		writeJSONError(response, http.StatusBadRequest, "replaySpeed должен быть от 0 до 1000")
		return
	}

	prepared := make([]preparedMockScenario, 0, len(batch.Scenarios))
	seen := make(map[string]struct{}, len(batch.Scenarios))
	for _, scenario := range batch.Scenarios {
		if err := scenario.validate(); err != nil {
			writeJSONError(response, http.StatusBadRequest, err.Error())
			return
		}
		if _, exists := seen[scenario.ScenarioID]; exists {
			writeJSONError(response, http.StatusBadRequest,
				fmt.Sprintf("scenarioId %q повторяется", scenario.ScenarioID))
			return
		}
		seen[scenario.ScenarioID] = struct{}{}
		currentDelay := scenario.PredictionTime.Sub(scenario.CurrentPlannedAt).Seconds()
		scenario.CurrentDelaySeconds = &currentDelay

		scenario.Evidence = deriveMockEvidence(scenario.TelemetryHistory, scenario.CurrentDelaySeconds)
		diagnosis := diagnoseMockEvidence(scenario.Evidence, scenario.CurrentDelaySeconds)
		if scenario.ExpectedReasonCode != "" && scenario.ExpectedReasonCode != diagnosis.code {
			writeJSONError(response, http.StatusUnprocessableEntity,
				fmt.Sprintf("сценарий %q: ожидалась причина %q, правила определили %q",
					scenario.ScenarioID, scenario.ExpectedReasonCode, diagnosis.code))
			return
		}
		prepared = append(prepared, preparedMockScenario{input: scenario, diagnosis: diagnosis})
	}

	results := make([]map[string]string, 0, len(prepared))
	for _, item := range prepared {
		results = append(results, map[string]string{
			"scenarioId": item.input.ScenarioID, "reasonCode": item.diagnosis.code, "reason": item.diagnosis.message,
		})
	}
	if err := a.runtime.ResetMockScenarios(seen, time.Now().UTC()); err != nil {
		writeJSONError(response, http.StatusInternalServerError, "не удалось сбросить предыдущие demo-сценарии")
		return
	}
	if batch.ReplaySpeed == 0 {
		now := time.Now().UTC()
		for _, item := range prepared {
			if err := a.applyMockScenario(item, now, batch.BindToLiveVehicle); err != nil {
				writeJSONError(response, http.StatusInternalServerError, "не удалось сохранить сценарий")
				return
			}
		}
	} else {
		a.scheduleMockScenarios(prepared, batch.ReplaySpeed, batch.BindToLiveVehicle)
	}
	writeJSON(response, http.StatusAccepted, map[string]any{
		"accepted": len(results), "replaySpeed": batch.ReplaySpeed, "results": results,
	})
}

func (a *API) scheduleMockScenarios(prepared []preparedMockScenario, speed float64, bindToLiveVehicle bool) {
	sort.SliceStable(prepared, func(i, j int) bool {
		return prepared[i].input.PredictionTime.Before(prepared[j].input.PredictionTime)
	})
	startedAt := time.Now().UTC()
	sourceStart := prepared[0].input.PredictionTime

	apply := func(item preparedMockScenario) {
		sourceHorizon := item.input.TargetPlannedAt.Sub(item.input.PredictionTime)
		sourceCurrentOffset := item.input.CurrentPlannedAt.Sub(item.input.PredictionTime)
		dueAt := startedAt.Add(scaleDuration(item.input.PredictionTime.Sub(sourceStart), speed))
		item.input.PredictionTime = dueAt
		// ReplaySpeed ускоряет появление новых ситуаций, но не меняет физический
		// размер задержки и ML-горизонт 10–15 минут.
		item.input.CurrentPlannedAt = dueAt.Add(sourceCurrentOffset)
		item.input.TargetPlannedAt = dueAt.Add(sourceHorizon)
		currentDelay := item.input.PredictionTime.Sub(item.input.CurrentPlannedAt).Seconds()
		item.input.CurrentDelaySeconds = &currentDelay
		if err := a.applyMockScenario(item, dueAt, bindToLiveVehicle); err != nil {
			slog.Error("не удалось применить mock-сценарий", "scenario_id", item.input.ScenarioID, "error", err)
		}
	}

	// Первый сценарий появляется вместе со стартом replay, остальные — только
	// когда наступает их момент на ускоренной шкале времени.
	apply(prepared[0])
	go func() {
		for _, item := range prepared[1:] {
			dueAt := startedAt.Add(scaleDuration(item.input.PredictionTime.Sub(sourceStart), speed))
			if wait := time.Until(dueAt); wait > 0 {
				timer := time.NewTimer(wait)
				<-timer.C
			}
			apply(item)
		}
	}()
}

func scaleDuration(value time.Duration, speed float64) time.Duration {
	return time.Duration(float64(value) / speed)
}

func (a *API) applyMockScenario(item preparedMockScenario, occurredAt time.Time, bindToLiveVehicle bool) error {
	scenario := item.input
	var boundVehicle models.VehicleState
	var hasBoundVehicle bool
	if bindToLiveVehicle {
		if vehicle, ok := a.runtime.Vehicle(scenario.UnitID); ok {
			boundVehicle, hasBoundVehicle = vehicle, true
			if vehicle.TRID != nil {
				scenario.TRID = *vehicle.TRID
			}
			if vehicle.RoutePatternID != nil {
				scenario.RoutePatternID = *vehicle.RoutePatternID
			}
			if vehicle.NextActionItemID != nil {
				scenario.TargetActionItemID = *vehicle.NextActionItemID
			}
			if vehicle.NextStop != nil {
				scenario.TargetStop = *vehicle.NextStop
			}
		} else {
			routePatternID := scenario.RoutePatternID
			trID := scenario.TRID
			actionItemID := scenario.TargetActionItemID
			vehicle := models.VehicleState{
				UnitID: scenario.UnitID, TRID: &trID,
				EventTime: occurredAt, ReceivedAt: occurredAt,
				Freshness: models.TelemetryLive, MatchStatus: models.MatchMatchedSpatial,
				RoutePatternID: &routePatternID, NextStop: &scenario.TargetStop,
				NextActionItemID: &actionItemID,
			}
			if latest, ok := latestMockTelemetry(scenario.TelemetryHistory); ok {
				vehicle.SpeedKmh = latest.SpeedKmh
				vehicle.HeadingDegrees = latest.HeadingDegrees
			}
			for _, stop := range a.init.Stops {
				if stop.ID == scenario.TargetStop.ID {
					position := stop.Position
					vehicle.Position = &position
					break
				}
			}
			boundVehicle, _ = a.runtime.PublishVehicle(vehicle, occurredAt)
			hasBoundVehicle = true
		}
	}
	predictedDelay := scenario.PredictedDelaySeconds
	if a.predictor != nil {
		point := mlclient.PredictionPoint{
			SampleID: "mock-" + scenario.ScenarioID, TRID: scenario.TRID,
			PredictionTime: scenario.PredictionTime, TargetStopID: scenario.TargetActionItemID,
			TargetTime: scenario.TargetPlannedAt,
		}
		if scenario.CurrentDelaySeconds != nil {
			point.CurrentDelay = *scenario.CurrentDelaySeconds
		}
		telemetry := buildMockTelemetry(scenario, boundVehicle, hasBoundVehicle)
		if len(telemetry) == 0 && hasBoundVehicle {
			row := mlclient.TelemetryPoint{
				TRID: scenario.TRID, EventTime: scenario.PredictionTime,
				LocationValid: boundVehicle.Position != nil,
			}
			if boundVehicle.Position != nil {
				lon, lat := boundVehicle.Position.Lon, boundVehicle.Position.Lat
				row.Lon, row.Lat = &lon, &lat
			}
			speed, heading := boundVehicle.SpeedKmh, boundVehicle.HeadingDegrees
			row.Speed, row.Heading = &speed, &heading
			telemetry = append(telemetry, row)
		}
		ctx, cancel := context.WithTimeout(context.Background(), a.predictionTimeout)
		results, err := a.predictor.PredictBatch(ctx, []mlclient.PredictionPoint{point}, telemetry)
		cancel()
		if err != nil {
			return fmt.Errorf("ML-прогноз mock-сценария %q: %w", scenario.ScenarioID, err)
		}
		value, ok := results[point.SampleID]
		if !ok {
			return fmt.Errorf("ML не вернул sample_id %q", point.SampleID)
		}
		predictedDelay = value
	}
	reason := item.diagnosis.message
	prediction := models.DelayPrediction{
		ID: "mock-" + scenario.ScenarioID, ScenarioID: scenario.ScenarioID,
		UnitID: scenario.UnitID, TRID: scenario.TRID, RoutePatternID: scenario.RoutePatternID,
		TargetActionItemID: scenario.TargetActionItemID, TargetStop: scenario.TargetStop,
		PredictionTime: scenario.PredictionTime, TargetPlannedAt: scenario.TargetPlannedAt,
		CurrentDelaySeconds:   scenario.CurrentDelaySeconds,
		PredictedDelaySeconds: predictedDelay, ReasonCode: item.diagnosis.code,
		Reason: &reason, Evidence: scenario.Evidence,
	}
	if err := a.runtime.AwaitOtherTargets(scenario.UnitID, scenario.TargetActionItemID, occurredAt); err != nil {
		return err
	}
	if scenario.CurrentDelaySeconds != nil {
		a.runtime.SetVehicleDelay(scenario.UnitID, *scenario.CurrentDelaySeconds, occurredAt)
	}
	return a.runtime.ApplyPrediction(prediction, a.incidentThreshold, occurredAt)
}

func (scenario mockScenario) validate() error {
	if !validScenarioID(scenario.ScenarioID) {
		return fmt.Errorf("scenarioId %q имеет неверный формат", scenario.ScenarioID)
	}
	if scenario.UnitID == 0 {
		return fmt.Errorf("unitId должен быть положительным")
	}
	if scenario.TRID <= 0 || scenario.TargetActionItemID <= 0 {
		return fmt.Errorf("trId и targetActionItemId должны быть положительными")
	}
	if strings.TrimSpace(scenario.RoutePatternID) == "" || len(scenario.RoutePatternID) > 128 {
		return fmt.Errorf("routePatternId обязателен и должен быть не длиннее 128 символов")
	}
	if strings.TrimSpace(scenario.TargetStop.ID) == "" || len(scenario.TargetStop.ID) > 128 {
		return fmt.Errorf("targetStop.id обязателен и должен быть не длиннее 128 символов")
	}
	if len(scenario.TargetStop.Address) > 256 {
		return fmt.Errorf("targetStop.address должен быть не длиннее 256 символов")
	}
	if scenario.PredictionTime.IsZero() || scenario.CurrentPlannedAt.IsZero() || scenario.TargetPlannedAt.IsZero() {
		return fmt.Errorf("predictionTime, currentPlannedAt и targetPlannedAt обязательны")
	}
	horizon := scenario.TargetPlannedAt.Sub(scenario.PredictionTime)
	if horizon < 10*time.Minute || horizon > 15*time.Minute {
		return fmt.Errorf("горизонт сценария должен быть от 10 до 15 минут")
	}
	if !finite(scenario.PredictedDelaySeconds) || math.Abs(scenario.PredictedDelaySeconds) > 900 {
		return fmt.Errorf("predictedDelaySeconds должен быть конечным числом в диапазоне -900..900")
	}
	currentDelay := scenario.PredictionTime.Sub(scenario.CurrentPlannedAt).Seconds()
	if !finite(currentDelay) || math.Abs(currentDelay) > 900 {
		return fmt.Errorf("разница predictionTime и currentPlannedAt должна быть в диапазоне -900..900 секунд")
	}
	if len(scenario.Evidence) > 16 {
		return fmt.Errorf("evidence не может содержать больше 16 признаков")
	}
	if len(scenario.TelemetryHistory) < 2 || len(scenario.TelemetryHistory) > 32 {
		return fmt.Errorf("telemetryHistory должен содержать от 2 до 32 точек")
	}
	seenOffsets := make(map[int]struct{}, len(scenario.TelemetryHistory))
	for _, sample := range scenario.TelemetryHistory {
		if sample.SecondsBefore < 0 || sample.SecondsBefore > 600 {
			return fmt.Errorf("telemetryHistory.secondsBefore должен быть в диапазоне 0..600")
		}
		if _, exists := seenOffsets[sample.SecondsBefore]; exists {
			return fmt.Errorf("telemetryHistory содержит повторный secondsBefore=%d", sample.SecondsBefore)
		}
		seenOffsets[sample.SecondsBefore] = struct{}{}
		if !finite(sample.SpeedKmh) || sample.SpeedKmh < 0 || sample.SpeedKmh > 150 ||
			!finite(sample.HeadingDegrees) || sample.HeadingDegrees < 0 || sample.HeadingDegrees > 360 {
			return fmt.Errorf("telemetryHistory содержит неверную скорость или курс")
		}
	}
	for name, value := range scenario.Evidence {
		if strings.TrimSpace(name) == "" || len(name) > 64 || !finite(value) {
			return fmt.Errorf("evidence содержит неверное поле %q или нечисловое значение", name)
		}
	}
	if scenario.ExpectedReasonCode != "" && !knownReasonCode(scenario.ExpectedReasonCode) {
		return fmt.Errorf("неизвестный expectedReasonCode %q", scenario.ExpectedReasonCode)
	}
	return nil
}

func latestMockTelemetry(history []mockTelemetrySample) (mockTelemetrySample, bool) {
	if len(history) == 0 {
		return mockTelemetrySample{}, false
	}
	latest := history[0]
	for _, sample := range history[1:] {
		if sample.SecondsBefore < latest.SecondsBefore {
			latest = sample
		}
	}
	return latest, true
}

func buildMockTelemetry(scenario mockScenario, vehicle models.VehicleState, hasVehicle bool) []mlclient.TelemetryPoint {
	if len(scenario.TelemetryHistory) == 0 || !hasVehicle {
		return nil
	}
	history := append([]mockTelemetrySample(nil), scenario.TelemetryHistory...)
	sort.Slice(history, func(i, j int) bool { return history[i].SecondsBefore > history[j].SecondsBefore })
	rows := make([]mlclient.TelemetryPoint, 0, len(history))
	for _, sample := range history {
		valid := sample.LocationValid == nil || *sample.LocationValid
		row := mlclient.TelemetryPoint{
			TRID: scenario.TRID, EventTime: scenario.PredictionTime.Add(-time.Duration(sample.SecondsBefore) * time.Second),
			LocationValid: valid,
		}
		speed, heading := sample.SpeedKmh, sample.HeadingDegrees
		row.Speed, row.Heading = &speed, &heading
		if valid && vehicle.Position != nil {
			// Приближение достаточно на городских дистанциях до 10 минут. Старые
			// точки лежат позади текущей по курсу и образуют связный трек.
			distanceM := speed / 3.6 * float64(sample.SecondsBefore)
			headingRad := heading * math.Pi / 180
			lat := vehicle.Position.Lat - distanceM*math.Cos(headingRad)/111_320
			lonScale := 111_320 * math.Cos(vehicle.Position.Lat*math.Pi/180)
			lon := vehicle.Position.Lon
			if math.Abs(lonScale) > 1 {
				lon -= distanceM * math.Sin(headingRad) / lonScale
			}
			row.Lon, row.Lat = &lon, &lat
		}
		rows = append(rows, row)
	}
	return rows
}

func validScenarioID(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, char := range value {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '-' && char != '_' {
			return false
		}
	}
	return true
}

func knownReasonCode(code string) bool {
	switch code {
	case "door_hold_delay", "traffic_slowdown", "poor_gps_quality",
		"route_deviation", "stop_dwell", "schedule_slippage", "insufficient_evidence":
		return true
	default:
		return false
	}
}

// deriveMockEvidence повторяет агрегацию диагностического сервиса над
// расширенной mock-телеметрией. Готовая причина в сценарии не передаётся.
func deriveMockEvidence(history []mockTelemetrySample, currentDelay *float64) map[string]float64 {
	evidence := map[string]float64{}
	if len(history) == 0 {
		return evidence
	}
	sorted := append([]mockTelemetrySample(nil), history...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].SecondsBefore > sorted[j].SecondsBefore })
	var speedSum float64
	valid := 0
	lastValidAge := math.Inf(1)
	var doorStart, stationaryStart *int
	for index := range sorted {
		sample := sorted[index]
		speedSum += sample.SpeedKmh
		isValid := sample.LocationValid == nil || *sample.LocationValid
		if isValid {
			valid++
			if float64(sample.SecondsBefore) < lastValidAge {
				lastValidAge = float64(sample.SecondsBefore)
			}
		}
		if sample.DoorOpen {
			if doorStart == nil {
				value := sample.SecondsBefore
				doorStart = &value
			}
		} else {
			doorStart = nil
		}
		if sample.SpeedKmh < 1 {
			if stationaryStart == nil {
				value := sample.SecondsBefore
				stationaryStart = &value
			}
		} else {
			stationaryStart = nil
		}
		if sample.CongestionIndex != nil {
			evidence["congestion_index"] = *sample.CongestionIndex
		}
		if sample.RouteDeviationMeters != nil {
			evidence["route_deviation_m"] = *sample.RouteDeviationMeters
		}
		if doorStart != nil {
			evidence["door_open_duration_s"] = float64(*doorStart - sample.SecondsBefore)
		}
		if stationaryStart != nil {
			evidence["stationary_duration_s"] = float64(*stationaryStart - sample.SecondsBefore)
		}
	}
	evidence["speed_mean_5m_kmh"] = speedSum / float64(len(sorted))
	evidence["valid_gps_points_5m"] = float64(valid)
	if !math.IsInf(lastValidAge, 1) {
		evidence["last_gps_age_s"] = lastValidAge
	}
	_ = currentDelay
	return evidence
}

func diagnoseMockEvidence(evidence map[string]float64, currentDelay *float64) mockDiagnosis {
	if value, ok := evidence["last_gps_age_s"]; ok && value >= 120 {
		return mockDiagnosis{"poor_gps_quality", "Последняя валидная GPS-точка устарела; причина задержки пока не подтверждена."}
	}
	if value, ok := evidence["valid_gps_points_5m"]; ok && value < 2 {
		return mockDiagnosis{"poor_gps_quality", "За последние 5 минут мало валидных GPS-точек; прогноз и определение причины менее надёжны."}
	}
	if value, ok := evidence["route_deviation_m"]; ok && value >= 100 {
		return mockDiagnosis{"route_deviation", "Положение ТС заметно отклоняется от коридора маршрута; проверьте сход с маршрута или качество геопозиции."}
	}
	if value, ok := evidence["door_open_duration_s"]; ok && value >= 90 {
		return mockDiagnosis{"door_hold_delay", "Двери оставались открыты дольше порога; вероятна дополнительная стоянка на остановке."}
	}
	if speed, hasSpeed := evidence["speed_mean_5m_kmh"]; hasSpeed && speed < 15 {
		if congestion, hasCongestion := evidence["congestion_index"]; hasCongestion && congestion >= 0.7 {
			return mockDiagnosis{"traffic_slowdown", "Низкая скорость совпадает с высоким индикатором загруженности участка; вероятно влияние трафика."}
		}
	}
	if value, ok := evidence["stationary_duration_s"]; ok && value >= 120 {
		return mockDiagnosis{"stop_dwell", "ТС долго не двигалось; без данных дверей и дорожного контекста точная причина стоянки неизвестна."}
	}
	if currentDelay != nil && *currentDelay >= 120 {
		return mockDiagnosis{"schedule_slippage", "Текущая задержка уже превышает 2 минуты; вероятно дальнейшее отставание от расписания."}
	}
	return mockDiagnosis{"insufficient_evidence", "Данных недостаточно, чтобы надёжно определить причину отклонения."}
}

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func writeJSONError(response http.ResponseWriter, status int, message string) {
	writeJSON(response, status, map[string]string{"error": message})
}
