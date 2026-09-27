package models

import "time"

// IncidentEventType описывает обнаруженное backend изменение инцидента. Это не
// операторский workflow: dashboard только добавляет, обновляет или удаляет
// элемент минимальной очереди инцидентов.
type IncidentEventType string

const (
	// IncidentNew добавляет впервые обнаруженную задержку в активную очередь.
	IncidentNew IncidentEventType = "new"
	// IncidentUpdated заменяет данные уже активного инцидента.
	IncidentUpdated IncidentEventType = "updated"
	// IncidentClosed удаляет инцидент, когда задержка перестала быть критичной.
	IncidentClosed IncidentEventType = "closed"
)

type IncidentStatus string

const (
	IncidentActive         IncidentStatus = "active"
	IncidentAwaitingResult IncidentStatus = "awaiting_result"
	IncidentResolved       IncidentStatus = "resolved"
	IncidentCancelled      IncidentStatus = "cancelled"
)

type IncidentOutcome string

const (
	IncidentOccurred    IncidentOutcome = "occurred"
	IncidentNotOccurred IncidentOutcome = "not_occurred"
)

// Incident — минимальный набор фактов для отображения задержанного ТС на карте
// и в очереди инцидентов. Критичность намеренно не хранится в модели: frontend
// вычисляет OK/WATCH/HIGH по PredictedDelaySeconds и порогам из DashboardInit.
type Incident struct {
	// ID — стабильный идентификатор, общий для событий new/updated/closed.
	ID string `json:"id"`
	// EventType указывает frontend: добавить, заменить или удалить элемент.
	EventType IncidentEventType `json:"eventType"`
	Status    IncidentStatus    `json:"status"`
	// UnitID идентифицирует конкретное ТС, для которого возник инцидент.
	UnitID uint32 `json:"unitId"`
	// TRID связывает инцидент с расписанием и ML; это не публичный номер маршрута.
	TRID int64 `json:"trId"`
	// RoutePatternID выбирает вычисленную линию маршрута для карты.
	RoutePatternID string `json:"routePatternId"`
	OccurrenceID   string `json:"occurrenceId"`
	// PredictionID ссылается на прогноз, создавший или обновивший инцидент.
	PredictionID string `json:"predictionId"`
	// TargetActionItemID идентифицирует целевое событие прибытия из расписания.
	TargetActionItemID int64 `json:"targetActionItemId"`
	// TargetStop — целевая остановка для строки или карточки инцидента.
	TargetStop StopReference `json:"targetStop"`
	// PredictedDelaySeconds — прогноз задержки; положительное значение означает опоздание.
	CurrentDelaySeconds   *float64 `json:"currentDelaySeconds,omitempty"`
	PredictedDelaySeconds float64  `json:"predictedDelaySeconds"`
	// PredictionTime — момент T, на который рассчитан связанный прогноз.
	PredictionTime             time.Time          `json:"predictionTime"`
	TargetPlannedAt            time.Time          `json:"targetPlannedAt"`
	FirstPredictedDelaySeconds float64            `json:"firstPredictedDelaySeconds"`
	ActualArrivalAt            *time.Time         `json:"actualArrivalAt,omitempty"`
	ActualDelaySeconds         *float64           `json:"actualDelaySeconds,omitempty"`
	Outcome                    *IncidentOutcome   `json:"outcome,omitempty"`
	ReasonCode                 string             `json:"reasonCode,omitempty"`
	Reason                     *string            `json:"reason,omitempty"`
	Evidence                   map[string]float64 `json:"evidence,omitempty"`
	ScenarioID                 string             `json:"scenarioId,omitempty"`
	// CreatedAt — время первого обнаружения непрерывного инцидента backend-сервисом.
	CreatedAt time.Time `json:"createdAt"`
	// UpdatedAt — время последнего изменения любого поля инцидента.
	UpdatedAt time.Time `json:"updatedAt"`
}

type IncidentHistoryPage struct {
	Items  []Incident `json:"items"`
	Total  int        `json:"total"`
	Limit  int        `json:"limit"`
	Offset int        `json:"offset"`
}
