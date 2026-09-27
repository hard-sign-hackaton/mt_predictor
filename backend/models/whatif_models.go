package models

import "time"

// WhatIfMode задаёт смысл сценария «выпуск дополнительного ТС».
//
// Оба режима описывают одну и ту же границу разбиения рейса, но дают
// принципиально разный эффект, поэтому режим обязан быть явным.
type WhatIfMode string

const (
	// WhatIfModeRelieve — рейс делится: действующее ТС ведёт calls[:k], а
	// кандидат забирает calls[k:]. Опубликованное расписание не меняется,
	// поэтому интервал обслуживания остаётся прежним. Реальный выигрыш — в
	// объёме работы и длительности рейса на одно ТС.
	WhatIfModeRelieve WhatIfMode = "relieve"
	// WhatIfModeDuplicate — кандидат выполняет calls[k:] параллельно
	// действующему ТС. Интервал обслуживания действительно сокращается, но
	// работа дублируется и стоимость эксплуатации удваивается.
	WhatIfModeDuplicate WhatIfMode = "duplicate"
)

// WhatIfVerdict — вывод о выполнимости сценария. Значения, связанные с
// недоступностью данных, возвращаются вместо расчётных чисел, чтобы
// frontend не трактовал отсутствие данных как нулевую задержку.
type WhatIfVerdict string

const (
	// WhatIfFeasible — ТС успевает к границе разбиения с запасом.
	WhatIfFeasible WhatIfVerdict = "feasible"
	// WhatIfTight — ТС не успевает, но отставание меньше пяти минут.
	WhatIfTight WhatIfVerdict = "tight"
	// WhatIfInfeasible — ТС не может подойти к границе разбиения вовремя.
	WhatIfInfeasible WhatIfVerdict = "infeasible"
	// WhatIfRunInPast — граница разбиения уже прошла, поэтому рейс нельзя
	// дополнить: вместо отрицательного запаса в сотни часов показывается это
	// состояние.
	WhatIfRunInPast WhatIfVerdict = "run_in_past"
	// WhatIfRunTooFar — граница разбиения дальше горизонта решения, поэтому
	// перегон сейчас не имеет смысла считать.
	WhatIfRunTooFar WhatIfVerdict = "run_too_far"
	// WhatIfUnknownVehicle — координаты ТС ещё не поступали.
	WhatIfUnknownVehicle WhatIfVerdict = "unknown_vehicle"
	// WhatIfNoPosition — последний пакет ТС имеет location_valid=false.
	WhatIfNoPosition WhatIfVerdict = "no_position"
	// WhatIfStalePosition — позиция старше TTL, расчёт приведён с оговоркой.
	WhatIfStalePosition WhatIfVerdict = "stale_position"
	// WhatIfEmptyOccurrence — у рейса нет событий расписания.
	WhatIfEmptyOccurrence WhatIfVerdict = "empty_occurrence"
)

const (
	// WhatIfReasonNoCapacityModel — причина, по которой сокращение задержки по
	// графику не вычисляется: в данных ровно одно ТС на паттерн маршрута, поэ-
	// тому модель пропускной способности отсутствует, а любая симуляция была бы
	// выдумкой.
	WhatIfReasonNoCapacityModel = "no_capacity_model"
	// WhatIfReasonNoPosition — у ТС нет пригодных координат для расчёта.
	WhatIfReasonNoPosition = "no_position"
	// WhatIfReasonUnknownVehicle — ТС ещё не появилось в потоке телеметрии.
	WhatIfReasonUnknownVehicle = "unknown_vehicle"
	// WhatIfReasonEmptyOccurrence — в рейсе нет событий расписания.
	WhatIfReasonEmptyOccurrence = "empty_occurrence"
	// WhatIfReasonRunInPast — граница разбиения прошла, запас не вычисляется.
	WhatIfReasonRunInPast = "run_in_past"
	// WhatIfReasonRunTooFar — граница разбиения за пределами горизонта решения.
	WhatIfReasonRunTooFar = "run_too_far"
)

// WhatIfReport — ответ read-only сценария. Все поля, которые невозможно
// вычислить, отсутствуют или равны null и никогда не заменяются нулём.
type WhatIfReport struct {
	// Mode — режим, по которому выполнено разбиение рейса.
	Mode WhatIfMode `json:"mode"`
	// Candidate — состояние ТС, рассматриваемого на выпуск.
	Candidate WhatIfCandidate `json:"candidate"`
	// Target — рейс и выбранная граница разбиения.
	Target WhatIfTarget `json:"target"`
	// Deadhead — перегон и вердикт о выполнимости.
	Deadhead WhatIfDeadhead `json:"deadhead"`
	// Conflict — пересечение с собственным расписанием кандидата.
	Conflict WhatIfConflict `json:"conflict"`
	// Partition — как рейс делится между двумя ТС.
	Partition WhatIfPartition `json:"partition"`
	// ServiceInterval — интервал обслуживания остановок до и после сценария.
	ServiceInterval WhatIfServiceInterval `json:"serviceInterval"`
	// ScheduleReliefSeconds всегда null: сокращение задержки по графику не
	// вычисляется. Поле присутствует, чтобы причина была видна явно.
	ScheduleReliefSeconds *float64 `json:"scheduleReliefSeconds"`
	// UnavailableReason объясняет, почему ScheduleReliefSeconds равен null.
	UnavailableReason string `json:"unavailableReason"`
	// Assumptions — допущения, при которых получены числа ответа.
	Assumptions []string `json:"assumptions"`
	// GeneratedAt — время формирования ответа на backend.
	GeneratedAt time.Time `json:"generatedAt"`
}

// WhatIfCandidate — снимок состояния ТС-кандидата на момент запроса.
type WhatIfCandidate struct {
	// UnitID идентифицирует движущийся маркер.
	UnitID uint32 `json:"unitId"`
	// TRID отсутствует, если unit не найден в таблице соответствий.
	TRID *int64 `json:"trId,omitempty"`
	// HasSchedule — есть ли опубликованное расписание у этого tr_id.
	HasSchedule *bool `json:"hasSchedule,omitempty"`
	// MatchStatus — результат связывания ТС с расписанием.
	MatchStatus MatchStatus `json:"matchStatus"`
	// Freshness — свежесть последнего пакета телеметрии.
	Freshness TelemetryFreshness `json:"freshness"`
	// Position отсутствует, если координаты невалидны или ещё не поступали.
	Position *GeoPoint `json:"position,omitempty"`
	// EventTime — момент последнего пакета телеметрии.
	EventTime time.Time `json:"eventTime"`
}

// WhatIfTarget — рейс, на который выпускается кандидат, и граница разбиения.
type WhatIfTarget struct {
	// OccurrenceID идентифицирует рейс с датой.
	OccurrenceID string `json:"occurrenceId"`
	// TRID — владелец рейса по расписанию.
	TRID int64 `json:"trId"`
	// RoutePatternID — внутренний идентификатор геометрии, не номер маршрута.
	RoutePatternID string `json:"routePatternId"`
	// JoinCallIndex — индекс события, с которого кандидат ведёт рейс.
	JoinCallIndex int `json:"joinCallIndex"`
	// JoinActionItemID — идентификатор события на границе разбиения.
	JoinActionItemID int64 `json:"joinActionItemId"`
	// JoinStopID — остановка на границе разбиения.
	JoinStopID string `json:"joinStopId"`
	// JoinStopAddress — читаемое имя остановки на границе разбиения.
	JoinStopAddress string `json:"joinStopAddress"`
	// JoinPlannedAt — плановое время события на границе разбиения.
	JoinPlannedAt time.Time `json:"joinPlannedAt"`
	// TotalCalls — число событий расписания в рейсе.
	TotalCalls int `json:"totalCalls"`
	// UniqueStops — число уникальных остановок в рейсе.
	UniqueStops int `json:"uniqueStops"`
}

// WhatIfDeadhead — перегон кандидата к границе разбиения и вывод о выполнимости.
type WhatIfDeadhead struct {
	// Verdict отсутствует, если расстояние вычислить невозможно.
	Verdict *WhatIfVerdict `json:"verdict,omitempty"`
	// Meters — расстояние от текущей позиции до события разбиения.
	Meters *float64 `json:"meters,omitempty"`
	// Seconds — оценка перегона при заданной скорости без пассажиров.
	Seconds *float64 `json:"seconds,omitempty"`
	// SlackSeconds — запас времени до планового события разбиения, отсчитанный
	// от момента фиксации позиции. Отсутствует, если граница разбиения прошла
	// или находится за пределами горизонта решения: разность моментов в этих
	// случаях выглядит числом, но лишена смысла.
	SlackSeconds *float64 `json:"slackSeconds,omitempty"`
	// AssumedEmptySpeedKmh — скорость, использованная для оценки перегона.
	AssumedEmptySpeedKmh float64 `json:"assumedEmptySpeedKmh"`
	// AvailableFrom — момент, когда позиция кандидата была зафиксирована и с
	// которого он может начать перегон. Это event_time телеметрии, а не стенные
	// часы сервера: иначе на историческом расписании разность с planned_at
	// измеряется месяцами.
	AvailableFrom *time.Time `json:"availableFrom,omitempty"`
	// RequiredBy — плановое время события разбиения.
	RequiredBy *time.Time `json:"requiredBy,omitempty"`
	// UnavailableReason объясняет отсутствие расчётных значений.
	UnavailableReason string `json:"unavailableReason,omitempty"`
}

// WhatIfConflict — пересечение сценария с собственным расписанием кандидата.
type WhatIfConflict struct {
	// Detected — найдено ли пересекающееся назначение того же tr_id.
	Detected bool `json:"detected"`
	// OccurrenceID — конфликтующий рейс.
	OccurrenceID string `json:"occurrenceId,omitempty"`
	// TRID подтверждает, что конфликт относится к самому кандидату.
	TRID int64 `json:"trId,omitempty"`
	// ValidFrom и ValidTo — окно конфликтующего рейса.
	ValidFrom *time.Time `json:"validFrom,omitempty"`
	ValidTo   *time.Time `json:"validTo,omitempty"`
}

// WhatIfPartition — как рейс делится между действующим ТС и кандидатом.
// Эти величины зависят только от границы разбиения и одинаковы в обоих режимах.
type WhatIfPartition struct {
	// CallsVehicleOne — число событий у действующего ТС.
	CallsVehicleOne int `json:"callsVehicleOne"`
	// CallsVehicleTwo — число событий у кандидата.
	CallsVehicleTwo int `json:"callsVehicleTwo"`
	// StopsServedByOne — остановки, обслуживаемые только одним ТС.
	StopsServedByOne int `json:"stopsServedByOne"`
	// StopsServedByTwo — остановки, обслуживаемые обоими ТС.
	StopsServedByTwo int `json:"stopsServedByTwo"`
	// FullRunSeconds — длительность рейса одним ТС до сценария.
	FullRunSeconds *float64 `json:"fullRunSeconds"`
	// LongestVehicleRunSeconds — длительность самого длинного рейса после
	// разбиения. Это единственная честная мера выигрыша в режиме relieve.
	LongestVehicleRunSeconds *float64 `json:"longestVehicleRunSeconds"`
}

// WhatIfServiceInterval — интервал обслуживания остановок до и после сценария.
// В режиме relieve эти значения равны: опубликованное расписание не меняется.
type WhatIfServiceInterval struct {
	// MeanHeadwayBeforeSeconds — средний интервал до сценария.
	MeanHeadwayBeforeSeconds *float64 `json:"meanHeadwayBeforeSeconds"`
	// MeanHeadwayAfterSeconds — средний интервал после сценария.
	MeanHeadwayAfterSeconds *float64 `json:"meanHeadwayAfterSeconds"`
	// ImprovedStops — остановки с наибольшим сокращением интервала. В режиме
	// relieve список пуст, потому что интервалы не меняются.
	ImprovedStops []WhatIfStopImpact `json:"improvedStops"`
}

// WhatIfStopImpact — влияние сценария на одну остановку.
type WhatIfStopImpact struct {
	// StopID ссылается на DashboardInit.Stops.
	StopID string `json:"stopId"`
	// Address — читаемое имя остановки.
	Address string `json:"address,omitempty"`
	// VisitsBefore и VisitsAfter — число визитов до и после сценария.
	VisitsBefore int `json:"visitsBefore"`
	VisitsAfter  int `json:"visitsAfter"`
	// HeadwayBeforeSeconds и HeadwayAfterSeconds — интервалы до и после.
	HeadwayBeforeSeconds *float64 `json:"headwayBeforeSeconds"`
	HeadwayAfterSeconds  *float64 `json:"headwayAfterSeconds"`
	// Reduction — доля сокращения интервала; в режиме relieve равна нулю.
	Reduction *float64 `json:"reduction"`
}
