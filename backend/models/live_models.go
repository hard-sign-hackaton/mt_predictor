package models

import "time"

// TelemetryFreshness вычисляется backend по EventTime и настроенному TTL.
type TelemetryFreshness string

const (
	// TelemetryLive означает, что возраст последнего пакета не превышает TTL.
	TelemetryLive TelemetryFreshness = "live"
	// TelemetryStale означает, что маркер можно показать, но данные уже неактуальны.
	TelemetryStale TelemetryFreshness = "stale"
)

// MatchStatus объясняет наличие или отсутствие маршрута и соседних остановок.
type MatchStatus string

const (
	// MatchMatched — ТС, активный паттерн и участок маршрута определены.
	MatchMatched MatchStatus = "matched"
	// MatchMatchedSpatial — историческое расписание не активно, поэтому паттерн
	// выбран только по известному TRID и расстоянию до геометрии маршрута.
	MatchMatchedSpatial MatchStatus = "matched_spatial"
	// MatchUnmappedUnit — unit_id отсутствует в VehicleBindings.
	MatchUnmappedUnit MatchStatus = "unmapped_unit"
	// MatchInvalidLocation — признак валидности координат NDTP равен false.
	MatchInvalidLocation MatchStatus = "invalid_location"
	// MatchNoSchedule — tr_id известен, но расписание для него отсутствует.
	MatchNoSchedule MatchStatus = "no_schedule"
	// MatchNoActivePattern — расписание есть, но EventTime не попадает ни в один рейс.
	MatchNoActivePattern MatchStatus = "no_active_pattern"
	// MatchOffRoute — точка находится за пределами допустимого коридора маршрута.
	MatchOffRoute MatchStatus = "off_route"
)

// StopReference — компактное представление остановки в потоковых данных.
type StopReference struct {
	// ID ссылается на DashboardInit.Stops.
	ID string `json:"id"`
	// Address дублируется, чтобы событие было читаемо до обращения к справочнику.
	Address string `json:"address"`
}

// MapSnapshot содержит только изменяемые данные рабочей карты. Статические
// маршруты и остановки frontend получает отдельно через DashboardInit.
type MapSnapshot struct {
	// Version — позиция потока событий, которой соответствует снимок.
	Version uint64 `json:"version"`
	// CreatedAt — время формирования снимка на backend.
	CreatedAt time.Time `json:"createdAt"`
	// Vehicles — последние известные состояния ТС, отсортированные по UnitID.
	Vehicles []VehicleState `json:"vehicles"`
}

// VehicleState — нормализованная телеметрия, дополненная map matching. ML-полей
// здесь нет, поэтому обновления координат не зависят от прогнозов.
type VehicleState struct {
	// UnitID идентифицирует движущийся маркер и берётся из NDTP peerAddress.
	UnitID uint32 `json:"unitId"`
	// TRID отсутствует, если unit не найден в таблице соответствий.
	TRID *int64 `json:"trId,omitempty"`
	// Position отсутствует при location_valid=false.
	Position *GeoPoint `json:"position,omitempty"`
	// SpeedKmh декодируется из G6CellNav00.speedAvg.
	SpeedKmh float64 `json:"speedKmh"`
	// HeadingDegrees декодируется из G6CellNav00.course, диапазон 0..360 градусов.
	HeadingDegrees float64 `json:"headingDegrees"`
	// EventTime декодируется из G6CellNav00.timestamp и задаёт порядок телеметрии.
	EventTime time.Time `json:"eventTime"`
	// ReceivedAt — время приёма backend, нужное для диагностики запоздалых пакетов.
	ReceivedAt time.Time `json:"receivedAt"`
	// Historical отличает историческую запись от текущей телеметрии.
	Historical bool `json:"historical"`
	// Freshness принимает live или stale согласно TTL backend.
	Freshness TelemetryFreshness `json:"freshness"`
	// MatchStatus описывает результат связывания ID и сопоставления с коридором.
	MatchStatus MatchStatus `json:"matchStatus"`
	// RoutePatternID присутствует после выбора активного паттерна маршрута.
	RoutePatternID *string `json:"routePatternId,omitempty"`
	// OccurrenceID идентифицирует выбранный рейс с датой.
	OccurrenceID *string `json:"occurrenceId,omitempty"`
	// PreviousStop присутствует после успешного определения участка.
	PreviousStop *StopReference `json:"previousStop,omitempty"`
	// NextStop присутствует после успешного определения участка.
	NextStop *StopReference `json:"nextStop,omitempty"`
	// NextActionItemID идентифицирует следующее событие расписания для ML и UI.
	NextActionItemID *int64 `json:"nextActionItemId,omitempty"`
	// DistanceToRouteMeters — расстояние до центральной линии маршрута в метрах.
	DistanceToRouteMeters *float64 `json:"distanceToRouteMeters,omitempty"`
	// GeometryQuality присутствует после выбора паттерна маршрута.
	GeometryQuality *GeometryQuality `json:"geometryQuality,omitempty"`
	// CurrentDelaySeconds появится после подключения расчёта задержки. Отсутствие
	// значения означает «нет данных» и не должно трактоваться как нулевая задержка.
	CurrentDelaySeconds *float64 `json:"currentDelaySeconds,omitempty"`
}

// DelayPrediction содержит гарантированные факты и необязательные дополнения.
// Frontend не должен придумывать отсутствующие confidence, reason или версию модели.
type DelayPrediction struct {
	// ID — sample_id либо другой стабильный идентификатор прогноза.
	ID string `json:"id"`
	// UnitID связывает прогноз с конкретным движущимся ТС.
	UnitID uint32 `json:"unitId"`
	// TRID связывает прогноз с расписанием и признаками модели.
	TRID int64 `json:"trId"`
	// RoutePatternID идентифицирует выбранный маршрутный контекст.
	RoutePatternID string `json:"routePatternId"`
	OccurrenceID   string `json:"occurrenceId"`
	// TargetActionItemID — это tt_action_item_id или target_stop_id.
	TargetActionItemID int64 `json:"targetActionItemId"`
	// TargetStop — целевая остановка, отображаемая в деталях инцидента.
	TargetStop StopReference `json:"targetStop"`
	// PredictionTime — момент T; входные данные обязаны удовлетворять event_time <= T.
	PredictionTime time.Time `json:"predictionTime"`
	// TargetPlannedAt — плановое прибытие в горизонте 10–15 минут.
	TargetPlannedAt time.Time `json:"targetPlannedAt"`
	// CurrentDelaySeconds — cur_dev_s, если значение доступно.
	CurrentDelaySeconds *float64 `json:"currentDelaySeconds,omitempty"`
	// PredictedDelaySeconds — результат ML: плюс означает опоздание, минус — опережение.
	PredictedDelaySeconds float64 `json:"predictedDelaySeconds"`
	// Confidence необязателен, поскольку текущий ответ ML его не содержит.
	Confidence *float64 `json:"confidence,omitempty"`
	// Reason необязателен и должен приходить от backend/ML, а не вычисляться frontend.
	Reason *string `json:"reason,omitempty"`
	// ReasonCode — машинный код возможной причины; frontend показывает Reason.
	ReasonCode string `json:"reasonCode,omitempty"`
	// Evidence — числовой снимок признаков, на которых основана диагностика.
	Evidence map[string]float64 `json:"evidence,omitempty"`
	// ScenarioID отмечает контролируемый тестовый сценарий.
	ScenarioID string `json:"scenarioId,omitempty"`
	// ModelVersion необязателен, пока версия не появилась в контракте ML.
	ModelVersion *string `json:"modelVersion,omitempty"`
}

// DashboardSnapshot восстанавливает только изменяемое состояние, нужное MVP
// карты и инцидентов после запуска или переподключения.
type DashboardSnapshot struct {
	// Version — монотонная позиция потока событий, представленная snapshot.
	Version uint64 `json:"version"`
	// CreatedAt — время создания snapshot на backend.
	CreatedAt time.Time `json:"createdAt"`
	// Vehicles содержит последнее состояние видимых реальных ТС.
	Vehicles []VehicleState `json:"vehicles"`
	// Predictions содержит последний пригодный прогноз для каждой цели и ТС.
	Predictions []DelayPrediction `json:"predictions"`
	// Incidents содержит текущее состояние минимальной очереди инцидентов.
	Incidents []Incident `json:"incidents"`
}

// LiveEventType определяет единственную полезную нагрузку LiveEvent.
type LiveEventType string

const (
	// LiveVehicleUpdated — обновилось состояние ТС на карте.
	LiveVehicleUpdated LiveEventType = "vehicle_updated"
	// LivePredictionUpdated — обновился прогноз задержки.
	LivePredictionUpdated LiveEventType = "prediction_updated"
	// LiveIncidentUpdated — изменилась очередь инцидентов.
	LiveIncidentUpdated LiveEventType = "incident_updated"
)

// LiveEvent — версионированный конверт SSE-события. EventID и Sequence позволяют
// frontend отбрасывать дубли и обнаруживать разрывы, требующие нового snapshot.
type LiveEvent struct {
	// EventID глобально уникален в пределах сохраняемого потока.
	EventID string `json:"eventId"`
	// Sequence монотонно растёт и нужен для порядка и обнаружения пропусков.
	Sequence uint64 `json:"sequence"`
	// Type указывает, какое необязательное поле содержит данные.
	Type LiveEventType `json:"type"`
	// OccurredAt — время создания события на backend.
	OccurredAt time.Time `json:"occurredAt"`
	// Vehicle присутствует для vehicle_updated.
	Vehicle *VehicleState `json:"vehicle,omitempty"`
	// Prediction присутствует для prediction_updated.
	Prediction *DelayPrediction `json:"prediction,omitempty"`
	// Incident присутствует для incident_updated.
	Incident *Incident `json:"incident,omitempty"`
}
