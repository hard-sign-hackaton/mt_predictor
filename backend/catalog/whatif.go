package catalog

import (
	"fmt"
	"math"
	"sort"
	"time"

	"mt_predictor/models"
)

const (
	// WhatIfDefaultEmptySpeedKmh — допущение о скорости перегона без пассажиров.
	// Значение возвращается в ответе, чтобы расчёт нельзя было принять за факт.
	WhatIfDefaultEmptySpeedKmh = 30.0
	// WhatIfTightSlackSeconds — нижняя граница тесного сценария. Отставание
	// меньше пяти минут ещё считается достижимым с оговоркой.
	WhatIfTightSlackSeconds = -300.0
	// WhatIfDefaultTopStops — сколько остановок с наибольшим сокращением
	// интервала возвращать по умолчанию.
	WhatIfDefaultTopStops = 8
	// WhatIfMaxJoinHorizon — предел, за которым запас по времени перестаёт быть
	// осмысленным: решение о выпуске ТС принимается не более чем за сутки, а
	// рейс, который начнётся позже, требует другого планирования.
	WhatIfMaxJoinHorizon = 24 * time.Hour
)

// WhatIfRequest описывает параметры сценария «выпуск дополнительного ТС».
// Позиция кандидата передаётся отдельным аргументом, чтобы расчёт оставался
// чистой функцией и не зависел от runtime.
type WhatIfRequest struct {
	// UnitID — рассматриваемое ТС.
	UnitID uint32
	// OccurrenceID — рейс, на который планируется выпуск.
	OccurrenceID string
	// JoinCallIndex — индекс события, с которого кандидат ведёт рейс.
	JoinCallIndex int
	// Mode — relieve или duplicate; неизвестное значение трактуется как relieve.
	Mode models.WhatIfMode
	// EmptySpeedKmh — скорость перегона без пассажиров; неположительное
	// значение заменяется на WhatIfDefaultEmptySpeedKmh.
	EmptySpeedKmh float64
	// TopStops — сколько остановок вернуть в ServiceInterval.ImprovedStops.
	TopStops int
}

// WhatIfCandidate — снимок состояния ТС-кандидата на момент запроса. Структура
// намеренно не зависит от dashboard, чтобы catalog не знал о runtime.
type WhatIfCandidate struct {
	// Known — пришло ли хотя бы одно состояние ТС из потока телеметрии.
	Known bool
	// TRID — владелец ТС; ноль означает отсутствие привязки в VehicleBindings.
	TRID int64
	// HasSchedule — есть ли опубликованное расписание у этого tr_id.
	HasSchedule bool
	// MatchStatus — результат связывания ТС с расписанием.
	MatchStatus models.MatchStatus
	// Freshness — свежесть последнего пакета телеметрии.
	Freshness models.TelemetryFreshness
	// Position — координаты ТС; nil означает непригодные или отсутствующие.
	Position *models.GeoPoint
	// EventTime — момент последнего пакета телеметрии.
	EventTime time.Time
}

// WhatIf вычисляет сценарий «выпуск дополнительного ТС» как чистую функцию от
// статического каталога и одного снимка состояния ТС. Метод не меняет
// каталог, не пишет в runtime и не порождает событий.
//
// Второй возвращаемый результат равен false только для неизвестного рейса,
// чтобы вызывающая сторона могла отличить 404 от расчёта с verdict.
func (c *Catalog) WhatIf(request WhatIfRequest, candidate WhatIfCandidate, now time.Time) (models.WhatIfReport, bool) {
	assignment, ok := c.assignmentByID[request.OccurrenceID]
	if !ok {
		return models.WhatIfReport{}, false
	}
	mode := request.Mode
	if mode != models.WhatIfModeDuplicate {
		mode = models.WhatIfModeRelieve
	}
	speed := request.EmptySpeedKmh
	if speed <= 0 {
		speed = WhatIfDefaultEmptySpeedKmh
	}
	top := request.TopStops
	if top <= 0 {
		top = WhatIfDefaultTopStops
	}
	assumptions := []string{
		fmt.Sprintf("скорость перегона без пассажиров %.0f км/ч", speed),
		"кандидат выполняет опубликованные события расписания без изменений",
	}

	if len(assignment.Events) == 0 {
		return models.WhatIfReport{
			Mode:      mode,
			Candidate: whatIfCandidateReport(request.UnitID, candidate),
			Target: models.WhatIfTarget{
				OccurrenceID: assignment.OccurrenceID, TRID: assignment.TRID,
				RoutePatternID: assignment.RoutePatternID,
			},
			Deadhead: models.WhatIfDeadhead{
				AssumedEmptySpeedKmh: speed, Verdict: whatIfVerdict(models.WhatIfEmptyOccurrence),
				UnavailableReason: models.WhatIfReasonEmptyOccurrence,
			},
			ServiceInterval:   models.WhatIfServiceInterval{ImprovedStops: []models.WhatIfStopImpact{}},
			UnavailableReason: models.WhatIfReasonNoCapacityModel,
			Assumptions:       assumptions,
			GeneratedAt:       now,
		}, true
	}

	target := c.whatIfTarget(assignment, request.JoinCallIndex)
	report := models.WhatIfReport{
		Mode:              mode,
		Candidate:         whatIfCandidateReport(request.UnitID, candidate),
		Target:            target,
		Deadhead:          c.whatIfDeadhead(candidate, assignment.Events[target.JoinCallIndex], speed, now),
		Conflict:          c.whatIfConflict(candidate, assignment, target.JoinPlannedAt),
		ServiceInterval:   models.WhatIfServiceInterval{ImprovedStops: []models.WhatIfStopImpact{}},
		UnavailableReason: models.WhatIfReasonNoCapacityModel,
		Assumptions:       assumptions,
		GeneratedAt:       now,
	}
	report.Partition, report.ServiceInterval = c.whatIfPartition(assignment, target.JoinCallIndex, mode, top)
	return report, true
}

func whatIfCandidateReport(unitID uint32, candidate WhatIfCandidate) models.WhatIfCandidate {
	result := models.WhatIfCandidate{
		UnitID: unitID, MatchStatus: candidate.MatchStatus, Freshness: candidate.Freshness,
		Position: candidate.Position, EventTime: candidate.EventTime,
	}
	if candidate.TRID > 0 {
		trID := candidate.TRID
		result.TRID = &trID
	}
	hasSchedule := candidate.HasSchedule
	result.HasSchedule = &hasSchedule
	return result
}

// whatIfTarget описывает рейс и границу разбиения. Индекс зажимается в
// границы событий, чтобы клиент не мог запросить несуществующий call.
func (c *Catalog) whatIfTarget(assignment RouteAssignment, requestedIndex int) models.WhatIfTarget {
	events := assignment.Events
	joinIndex := requestedIndex
	if joinIndex > len(events)-1 {
		joinIndex = len(events) - 1
	}
	if joinIndex < 0 {
		joinIndex = 0
	}
	join := events[joinIndex]
	unique := make(map[string]struct{}, len(events))
	for _, event := range events {
		unique[event.StopID] = struct{}{}
	}
	target := models.WhatIfTarget{
		OccurrenceID: assignment.OccurrenceID, TRID: assignment.TRID,
		RoutePatternID: assignment.RoutePatternID, JoinCallIndex: joinIndex,
		JoinActionItemID: join.ActionItemID, JoinStopID: join.StopID,
		JoinPlannedAt: join.PlannedAt, TotalCalls: len(events), UniqueStops: len(unique),
	}
	if stop, ok := c.stopByID[join.StopID]; ok {
		target.JoinStopAddress = stop.Address
	}
	return target
}

// whatIfDeadhead оценивает перегон кандидата к границе разбиения. Отсутствие
// данных выражается отсутствующими числами и кодом причины, а не нулём.
func (c *Catalog) whatIfDeadhead(candidate WhatIfCandidate, join ScheduleEvent, speedKmh float64, now time.Time) models.WhatIfDeadhead {
	// Опорное время — момент, когда была зафиксирована позиция кандидата, а не
	// стенные часы сервера. Только так «осталось ли время дойти» является
	// осмысленным вопросом: planned_at рейса и event_time телеметрии приходят
	// из одного источника и живут в одной шкале времени, тогда как now при
	// историческом расписании отстоял от них на месяцы и давал запас в тысячи
	// часов. now используется только для GeneratedAt.
	availableFrom := candidate.EventTime
	if availableFrom.IsZero() {
		availableFrom = now
	}
	requiredBy := join.PlannedAt
	result := models.WhatIfDeadhead{
		AssumedEmptySpeedKmh: speedKmh, AvailableFrom: &availableFrom, RequiredBy: &requiredBy,
	}
	switch {
	case !candidate.Known:
		result.Verdict = whatIfVerdict(models.WhatIfUnknownVehicle)
		result.UnavailableReason = models.WhatIfReasonUnknownVehicle
		return result
	case candidate.Position == nil:
		result.Verdict = whatIfVerdict(models.WhatIfNoPosition)
		result.UnavailableReason = models.WhatIfReasonNoPosition
		return result
	}
	meters := whatIfDistanceMeters(candidate.Position.Lon, candidate.Position.Lat, join.Lon, join.Lat)
	seconds := meters / (speedKmh / 3.6)
	result.Meters, result.Seconds = &meters, &seconds
	// Запас по времени осмыслен, пока граница разбиения впереди позиции
	// кандидата и находится в пределах горизонта решения. Вне диапазона
	// разность моментов выглядит правдоподобным числом, но ничего не значит,
	// поэтому не вычисляется вовсе.
	horizon := requiredBy.Sub(availableFrom)
	switch {
	case horizon < 0:
		result.Verdict = whatIfVerdict(models.WhatIfRunInPast)
		result.UnavailableReason = models.WhatIfReasonRunInPast
		return result
	case horizon > WhatIfMaxJoinHorizon:
		result.Verdict = whatIfVerdict(models.WhatIfRunTooFar)
		result.UnavailableReason = models.WhatIfReasonRunTooFar
		return result
	}
	slack := horizon.Seconds() - seconds
	result.SlackSeconds = &slack
	switch {
	case slack >= 0:
		result.Verdict = whatIfVerdict(models.WhatIfFeasible)
	case slack >= WhatIfTightSlackSeconds:
		result.Verdict = whatIfVerdict(models.WhatIfTight)
	default:
		result.Verdict = whatIfVerdict(models.WhatIfInfeasible)
	}
	if candidate.Freshness == models.TelemetryStale {
		// Расчёт приведён с оговоркой: позиция старше TTL.
		result.Verdict = whatIfVerdict(models.WhatIfStalePosition)
	}
	return result
}

// whatIfConflict ищет пересечение сценария с собственным расписанием
// кандидата. Тот же рейс конфликтом не считается: это не накладка, а его
// собственное назначение.
func (c *Catalog) whatIfConflict(candidate WhatIfCandidate, assignment RouteAssignment, windowFrom time.Time) models.WhatIfConflict {
	if !candidate.Known || candidate.TRID == 0 {
		return models.WhatIfConflict{}
	}
	windowTo := assignment.ValidTo
	for _, other := range c.assignmentsByTR[candidate.TRID] {
		if other.OccurrenceID == assignment.OccurrenceID {
			continue
		}
		if !other.ValidFrom.Before(windowTo) || !other.ValidTo.After(windowFrom) {
			continue
		}
		validFrom, validTo := other.ValidFrom, other.ValidTo
		return models.WhatIfConflict{
			Detected: true, OccurrenceID: other.OccurrenceID, TRID: candidate.TRID,
			ValidFrom: &validFrom, ValidTo: &validTo,
		}
	}
	return models.WhatIfConflict{}
}

// whatIfVisit — одно посещение остановки с индексом события в рейсе. Индекс
// нужен, чтобы отличить часть рейса до и после границы разбиения.
type whatIfVisit struct {
	index int
	at    time.Time
}

// whatIfPartition описывает, как рейс делится между двумя ТС, и пересчитывает
// интервал обслуживания остановок.
//
// Различие режимов нельзя смешивать: при relieve опубликованное расписание не
// меняется, поэтому интервал остаётся прежним, а выигрыш есть только в объёме
// работы на одно ТС. При duplicate кандидат выполняет те же события
// параллельно, поэтому интервал действительно сокращается.
func (c *Catalog) whatIfPartition(assignment RouteAssignment, joinIndex int, mode models.WhatIfMode, top int) (models.WhatIfPartition, models.WhatIfServiceInterval) {
	events := assignment.Events
	partition := models.WhatIfPartition{
		CallsVehicleOne: joinIndex, CallsVehicleTwo: len(events) - joinIndex,
		FullRunSeconds:           whatIfSpanSeconds(events),
		LongestVehicleRunSeconds: whatIfLongestRunSeconds(events, joinIndex, mode),
	}
	byStop := make(map[string][]whatIfVisit, len(events))
	for index, event := range events {
		byStop[event.StopID] = append(byStop[event.StopID], whatIfVisit{index: index, at: event.PlannedAt})
	}
	impacts := make([]models.WhatIfStopImpact, 0, len(byStop))
	beforeSum, afterSum, counted := 0.0, 0.0, 0
	for stopID, visits := range byStop {
		first, second := whatIfVehicleCoverage(visits, joinIndex)
		if first && second {
			partition.StopsServedByTwo++
		} else {
			partition.StopsServedByOne++
		}
		before := whatIfMeanHeadway(visits, joinIndex, mode, false)
		after := whatIfMeanHeadway(visits, joinIndex, mode, true)
		if before == nil || after == nil {
			continue
		}
		beforeSum, afterSum, counted = beforeSum+*before, afterSum+*after, counted+1
		if *after >= *before {
			continue
		}
		reduction := 1 - *after / *before
		impact := models.WhatIfStopImpact{
			StopID: stopID, VisitsBefore: len(visits),
			VisitsAfter:          whatIfVisitsAfter(visits, joinIndex, mode),
			HeadwayBeforeSeconds: before, HeadwayAfterSeconds: after, Reduction: &reduction,
		}
		if stop, ok := c.stopByID[stopID]; ok {
			impact.Address = stop.Address
		}
		impacts = append(impacts, impact)
	}
	interval := models.WhatIfServiceInterval{ImprovedStops: []models.WhatIfStopImpact{}}
	if counted > 0 {
		meanBefore, meanAfter := beforeSum/float64(counted), afterSum/float64(counted)
		interval.MeanHeadwayBeforeSeconds, interval.MeanHeadwayAfterSeconds = &meanBefore, &meanAfter
	}
	sort.Slice(impacts, func(i, j int) bool {
		if *impacts[i].Reduction != *impacts[j].Reduction {
			return *impacts[i].Reduction > *impacts[j].Reduction
		}
		return impacts[i].StopID < impacts[j].StopID
	})
	if len(impacts) > top {
		impacts = impacts[:top]
	}
	interval.ImprovedStops = impacts
	return partition, interval
}

// whatIfMeanHeadway считает средний интервал между соседними посещениями
// остановки. При after=true учитывается вклад второго ТС согласно режиму.
func whatIfMeanHeadway(visits []whatIfVisit, joinIndex int, mode models.WhatIfMode, after bool) *float64 {
	times := make([]time.Time, 0, len(visits)*2)
	for _, visit := range visits {
		times = append(times, visit.at)
		if after && whatIfDuplicated(visit, joinIndex, mode) {
			times = append(times, visit.at)
		}
	}
	if len(times) < 2 {
		return nil
	}
	sort.Slice(times, func(i, j int) bool { return times[i].Before(times[j]) })
	span := times[len(times)-1].Sub(times[0]).Seconds()
	if span <= 0 {
		return nil
	}
	headway := span / float64(len(times)-1)
	return &headway
}

// whatIfDuplicated сообщает, обслуживает ли остановку второй ТС в этой части
// рейса. При relieve опубликованные визиты не дублируются, поэтому порядок
// посещений пассажира не меняется.
func whatIfDuplicated(visit whatIfVisit, joinIndex int, mode models.WhatIfMode) bool {
	return mode == models.WhatIfModeDuplicate && visit.index >= joinIndex
}

// whatIfVisitsAfter возвращает число обслуживаний остановки после сценария.
func whatIfVisitsAfter(visits []whatIfVisit, joinIndex int, mode models.WhatIfMode) int {
	total := len(visits)
	for _, visit := range visits {
		if whatIfDuplicated(visit, joinIndex, mode) {
			total++
		}
	}
	return total
}

// whatIfVehicleCoverage сообщает, обслуживают ли остановку оба ТС после
// разбиения или только один. Остановка, у которой все визиты попали на одну
// половину рейса, обслуживается ровно одним ТС.
func whatIfVehicleCoverage(visits []whatIfVisit, joinIndex int) (first, second bool) {
	for _, visit := range visits {
		if visit.index >= joinIndex {
			second = true
			continue
		}
		first = true
	}
	return first, second
}

// whatIfSpanSeconds возвращает длительность участка расписания. Участок из
// одного события не имеет длительности, поэтому возвращается nil.
func whatIfSpanSeconds(events []ScheduleEvent) *float64 {
	if len(events) < 2 {
		return nil
	}
	span := events[len(events)-1].PlannedAt.Sub(events[0].PlannedAt).Seconds()
	if span <= 0 {
		return nil
	}
	return &span
}

// whatIfLongestRunSeconds возвращает длительность самого длинного рейса из
// двух после разбиения. Это единственная честная мера выигрыша при relieve.
func whatIfLongestRunSeconds(events []ScheduleEvent, joinIndex int, mode models.WhatIfMode) *float64 {
	first, second := whatIfSpanSeconds(events[:joinIndex]), whatIfSpanSeconds(events[joinIndex:])
	if mode == models.WhatIfModeDuplicate {
		// Кандидат дублирует вторую половину, поэтому действующее ТС выполняет
		// рейс целиком и самый длинный рейс не сокращается.
		return whatIfSpanSeconds(events)
	}
	switch {
	case first == nil && second == nil:
		return nil
	case first == nil:
		return second
	case second == nil:
		return first
	case *second > *first:
		return second
	default:
		return first
	}
}

// WhatIfCandidateState собирает снимок кандидата из runtime и таблицы
// соответствий. Отсутствие ТС в потоке телеметрии не считается ошибкой:
// возвращается Known=false, чтобы вызывающая сторона показала пустое состояние
// вместо HTTP-ошибки.
func (c *Catalog) WhatIfCandidateState(unitID uint32, vehicle models.VehicleState, known bool) WhatIfCandidate {
	candidate := WhatIfCandidate{
		Known: known, MatchStatus: vehicle.MatchStatus, Freshness: vehicle.Freshness,
		Position: vehicle.Position, EventTime: vehicle.EventTime,
	}
	if trID, ok := c.TRIDForUnit(unitID); ok {
		candidate.TRID = trID
	}
	for _, binding := range c.VehicleBindings {
		if binding.UnitID == unitID {
			candidate.HasSchedule = binding.HasSchedule
			break
		}
	}
	return candidate
}

// whatIfDistanceMeters — расстояние между двумя точками в той же московской
// проекции, что и map matching. Константы намеренно не дублируются.
func whatIfDistanceMeters(lonA, latA, lonB, latB float64) float64 {
	return math.Hypot((lonA-lonB)*moscowLonScale, (latA-latB)*latScale)
}

func whatIfVerdict(verdict models.WhatIfVerdict) *models.WhatIfVerdict { return &verdict }
