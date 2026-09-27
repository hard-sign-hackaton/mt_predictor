package dashboard

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"mt_predictor/models"
)

const (
	maxMockScenarioBytes = 1 << 20
	maxMockScenarioCount = 32
	mockUnitIDMin        = 990000000
	mockUnitIDMax        = 999999999
)

type mockScenarioBatch struct {
	Scenarios []mockScenario `json:"scenarios"`
}

type mockScenario struct {
	ScenarioID            string               `json:"scenarioId"`
	UnitID                uint32               `json:"unitId"`
	TRID                  int64                `json:"trId"`
	RoutePatternID        string               `json:"routePatternId"`
	TargetActionItemID    int64                `json:"targetActionItemId"`
	TargetStop            models.StopReference `json:"targetStop"`
	PredictionTime        time.Time            `json:"predictionTime"`
	TargetPlannedAt       time.Time            `json:"targetPlannedAt"`
	CurrentDelaySeconds   *float64             `json:"currentDelaySeconds,omitempty"`
	PredictedDelaySeconds float64              `json:"predictedDelaySeconds"`
	ExpectedReasonCode    string               `json:"expectedReasonCode,omitempty"`
	Evidence              map[string]float64   `json:"evidence"`
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

	type preparedScenario struct {
		input     mockScenario
		diagnosis mockDiagnosis
	}
	prepared := make([]preparedScenario, 0, len(batch.Scenarios))
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

		diagnosis := diagnoseMockEvidence(scenario.Evidence, scenario.CurrentDelaySeconds)
		if scenario.ExpectedReasonCode != "" && scenario.ExpectedReasonCode != diagnosis.code {
			writeJSONError(response, http.StatusUnprocessableEntity,
				fmt.Sprintf("сценарий %q: ожидалась причина %q, правила определили %q",
					scenario.ScenarioID, scenario.ExpectedReasonCode, diagnosis.code))
			return
		}
		prepared = append(prepared, preparedScenario{input: scenario, diagnosis: diagnosis})
	}

	now := time.Now().UTC()
	results := make([]map[string]string, 0, len(prepared))
	for _, item := range prepared {
		scenario := item.input
		reason := item.diagnosis.message
		prediction := models.DelayPrediction{
			ID: "mock-" + scenario.ScenarioID, ScenarioID: scenario.ScenarioID,
			UnitID: scenario.UnitID, TRID: scenario.TRID, RoutePatternID: scenario.RoutePatternID,
			TargetActionItemID: scenario.TargetActionItemID, TargetStop: scenario.TargetStop,
			PredictionTime: scenario.PredictionTime, TargetPlannedAt: scenario.TargetPlannedAt,
			CurrentDelaySeconds:   scenario.CurrentDelaySeconds,
			PredictedDelaySeconds: scenario.PredictedDelaySeconds, ReasonCode: item.diagnosis.code,
			Reason: &reason, Evidence: scenario.Evidence,
		}
		if err := a.runtime.ApplyPrediction(prediction, a.incidentThreshold, now); err != nil {
			writeJSONError(response, http.StatusInternalServerError, "не удалось сохранить сценарий")
			return
		}
		results = append(results, map[string]string{
			"scenarioId": scenario.ScenarioID, "reasonCode": item.diagnosis.code, "reason": reason,
		})
	}
	writeJSON(response, http.StatusAccepted, map[string]any{
		"accepted": len(results), "results": results,
	})
}

func (scenario mockScenario) validate() error {
	if !validScenarioID(scenario.ScenarioID) {
		return fmt.Errorf("scenarioId %q имеет неверный формат", scenario.ScenarioID)
	}
	if scenario.UnitID < mockUnitIDMin || scenario.UnitID > mockUnitIDMax {
		return fmt.Errorf("unitId должен быть в зарезервированном диапазоне %d..%d",
			mockUnitIDMin, mockUnitIDMax)
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
	if scenario.PredictionTime.IsZero() || scenario.TargetPlannedAt.IsZero() {
		return fmt.Errorf("predictionTime и targetPlannedAt обязательны")
	}
	horizon := scenario.TargetPlannedAt.Sub(scenario.PredictionTime)
	if horizon < 10*time.Minute || horizon > 15*time.Minute {
		return fmt.Errorf("горизонт сценария должен быть от 10 до 15 минут")
	}
	if !finite(scenario.PredictedDelaySeconds) || math.Abs(scenario.PredictedDelaySeconds) > 900 {
		return fmt.Errorf("predictedDelaySeconds должен быть конечным числом в диапазоне -900..900")
	}
	if scenario.CurrentDelaySeconds != nil &&
		(!finite(*scenario.CurrentDelaySeconds) || math.Abs(*scenario.CurrentDelaySeconds) > 900) {
		return fmt.Errorf("currentDelaySeconds должен быть конечным числом в диапазоне -900..900")
	}
	if len(scenario.Evidence) > 16 {
		return fmt.Errorf("evidence не может содержать больше 16 признаков")
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
