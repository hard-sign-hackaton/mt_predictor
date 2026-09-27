package dashboard

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"mt_predictor/catalog"
	"mt_predictor/internal/mlclient"
	"mt_predictor/internal/ndtp"
	"mt_predictor/models"
)

// Processor связывает NDTP, расписание, feature pipeline и ML. Сетевой приёмник
// только передаёт ему декодированный пакет и никогда не ждёт gRPC-ответ.
type Processor struct {
	catalog           *catalog.Catalog
	matcher           *catalog.Matcher
	runtime           *Runtime
	predictor         mlclient.Predictor
	predictionEvery   time.Duration
	predictionTimeout time.Duration
	incidentThreshold float64
	logger            *slog.Logger

	mu       sync.Mutex
	progress map[uint32]*vehicleProgress
	history  map[int64][]mlclient.TelemetryPoint
	pending  map[uint32]predictionCandidate
	wake     chan struct{}
}

type vehicleProgress struct {
	lastEventTime    time.Time
	occurrenceID     string
	confirmedSegment int
	candidateSegment int
	candidateCount   int
	candidateFirstAt time.Time
	currentDelay     *float64
	lastAttemptAt    time.Time
	lastTargetAction int64
}

type predictionCandidate struct {
	point          mlclient.PredictionPoint
	telemetry      []mlclient.TelemetryPoint
	unitID         uint32
	routePatternID string
	occurrenceID   string
	targetStop     models.StopReference
}

// ProcessorOptions задаёт периодичность прогноза, таймаут вызова ML и порог,
// при превышении которого задержка превращается в инцидент. Нулевые
// значения заменяются значениями по умолчанию.
type ProcessorOptions struct {
	PredictionEvery   time.Duration
	PredictionTimeout time.Duration
	IncidentThreshold float64
	Logger            *slog.Logger
}

func NewProcessor(routeCatalog *catalog.Catalog, runtime *Runtime, predictor mlclient.Predictor, options ProcessorOptions) *Processor {
	if options.PredictionEvery <= 0 {
		options.PredictionEvery = 5 * time.Minute
	}
	if options.PredictionTimeout <= 0 {
		options.PredictionTimeout = 3 * time.Second
	}
	if options.IncidentThreshold <= 0 {
		options.IncidentThreshold = 120
	}
	if options.Logger == nil {
		options.Logger = slog.Default()
	}
	return &Processor{
		catalog: routeCatalog, matcher: catalog.NewMatcher(routeCatalog), runtime: runtime, predictor: predictor,
		predictionEvery: options.PredictionEvery, predictionTimeout: options.PredictionTimeout,
		incidentThreshold: options.IncidentThreshold, logger: options.Logger,
		progress: make(map[uint32]*vehicleProgress), history: make(map[int64][]mlclient.TelemetryPoint),
		pending: make(map[uint32]predictionCandidate), wake: make(chan struct{}, 1),
	}
}

// Run обрабатывает накопленные цели микробатчами до отмены контекста.
func (p *Processor) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-p.wake:
			timer := time.NewTimer(100 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			p.flush(ctx)
		}
	}
}

// Apply нормализует один NDTP-пакет, обновляет карту и при наличии честного
// cur_dev_s ставит прогноз в асинхронную очередь.
func (p *Processor) Apply(point ndtp.TelemetryPoint) models.VehicleState {
	lon, lat, valid := point.Nav.Position()
	telemetry := catalog.Telemetry{
		UnitID: point.VehicleID, Lon: lon, Lat: lat, Speed: point.Nav.SpeedAvg, Heading: point.Nav.Course,
		EventTime: point.Nav.Timestamp, ReceiveTime: point.ReceivedAt, Valid: valid,
	}
	match := p.matcher.MatchLive(telemetry)

	p.mu.Lock()
	progress := p.progress[point.VehicleID]
	if progress == nil {
		progress = &vehicleProgress{confirmedSegment: -1, candidateSegment: -1}
		p.progress[point.VehicleID] = progress
	}
	if !progress.lastEventTime.IsZero() && telemetry.EventTime.Before(progress.lastEventTime) {
		p.mu.Unlock()
		if current, ok := p.runtime.Vehicle(point.VehicleID); ok {
			return current
		}
	}
	progress.lastEventTime = telemetry.EventTime

	if match.TRID != 0 {
		p.appendHistory(match.TRID, point, valid, lon, lat)
	}
	arrival := p.updateDelay(progress, match, telemetry.EventTime)
	if arrival != nil {
		if err := p.runtime.ResolveArrival(point.VehicleID, arrival.actionItemID, arrival.arrivalAt, arrival.delaySeconds, p.incidentThreshold, point.ReceivedAt); err != nil {
			p.logger.Error("не удалось зафиксировать результат инцидента", "error", err)
		}
	}
	vehicle := catalog.VehicleState(telemetry, match, models.TelemetryLive)
	if progress.currentDelay != nil {
		delay := *progress.currentDelay
		vehicle.CurrentDelaySeconds = &delay
	}

	var candidate *predictionCandidate
	if p.predictor != nil && match.Status == catalog.MatchMatched && progress.currentDelay != nil {
		if assignment, ok := p.catalog.Assignment(match.OccurrenceID); ok {
			if target, ok := selectTarget(assignment.Events, telemetry.EventTime); ok {
				if err := p.runtime.AwaitOtherTargets(point.VehicleID, target.ActionItemID, point.ReceivedAt); err != nil {
					p.logger.Error("не удалось обновить цели инцидентов", "error", err)
				}
				changedTarget := progress.lastTargetAction != target.ActionItemID
				if changedTarget || progress.lastAttemptAt.IsZero() || telemetry.EventTime.Sub(progress.lastAttemptAt) >= p.predictionEvery {
					progress.lastAttemptAt = telemetry.EventTime
					progress.lastTargetAction = target.ActionItemID
					stop, _ := p.catalog.Stop(target.StopID)
					value := predictionCandidate{
						point: mlclient.PredictionPoint{
							SampleID: fmt.Sprintf("live-%d-%d-%d", point.VehicleID, target.ActionItemID, telemetry.EventTime.Unix()),
							TRID:     match.TRID, PredictionTime: telemetry.EventTime,
							TargetStopID: target.ActionItemID, TargetTime: target.PlannedAt,
							CurrentDelay: *progress.currentDelay,
						},
						telemetry: append([]mlclient.TelemetryPoint(nil), p.history[match.TRID]...),
						unitID:    point.VehicleID, routePatternID: match.RoutePatternID, occurrenceID: match.OccurrenceID,
						targetStop: models.StopReference{ID: target.StopID, Address: stop.Address},
					}
					candidate = &value
				}
			} else {
				if err := p.runtime.AwaitOtherTargets(point.VehicleID, 0, point.ReceivedAt); err != nil {
					p.logger.Error("не удалось перевести инциденты в ожидание", "error", err)
				}
				progress.lastTargetAction = 0
			}
		}
	}
	if candidate != nil {
		p.pending[point.VehicleID] = *candidate
	}
	p.mu.Unlock()

	current, _ := p.runtime.PublishVehicle(vehicle, point.ReceivedAt)
	if candidate != nil {
		select {
		case p.wake <- struct{}{}:
		default:
		}
	}
	return current
}

type confirmedArrival struct {
	actionItemID int64
	arrivalAt    time.Time
	delaySeconds float64
}

func (p *Processor) updateDelay(progress *vehicleProgress, match catalog.MatchResult, eventTime time.Time) *confirmedArrival {
	if match.Status != catalog.MatchMatched {
		return nil
	}
	if progress.occurrenceID != match.OccurrenceID {
		progress.occurrenceID = match.OccurrenceID
		progress.confirmedSegment = match.SegmentIndex
		progress.candidateSegment = -1
		progress.candidateCount = 0
		progress.currentDelay = nil
		progress.lastAttemptAt = time.Time{}
		progress.lastTargetAction = 0
		return nil
	}
	if match.SegmentIndex <= progress.confirmedSegment {
		progress.candidateSegment = -1
		progress.candidateCount = 0
		return nil
	}
	if progress.candidateSegment != match.SegmentIndex {
		progress.candidateSegment = match.SegmentIndex
		progress.candidateCount = 1
		progress.candidateFirstAt = eventTime
		return nil
	}
	progress.candidateCount++
	if progress.candidateCount < 2 || match.PreviousPlannedAt.IsZero() {
		return nil
	}
	delay := progress.candidateFirstAt.Sub(match.PreviousPlannedAt).Seconds()
	progress.currentDelay = &delay
	progress.confirmedSegment = match.SegmentIndex
	progress.candidateSegment = -1
	progress.candidateCount = 0
	return &confirmedArrival{actionItemID: match.PreviousActionItemID, arrivalAt: progress.candidateFirstAt, delaySeconds: delay}
}

func (p *Processor) appendHistory(trID int64, point ndtp.TelemetryPoint, valid bool, lon, lat float64) {
	row := mlclient.TelemetryPoint{TRID: trID, EventTime: point.Nav.Timestamp, LocationValid: valid}
	row.Speed = finitePointer(point.Nav.SpeedAvg)
	row.Heading = finitePointer(point.Nav.Course)
	row.Alt = finitePointer(point.Nav.Altitude)
	if valid {
		row.Lon, row.Lat = finitePointer(lon), finitePointer(lat)
	}
	rows := append(p.history[trID], row)
	cutoff := row.EventTime.Add(-10 * time.Minute)
	firstRecent := len(rows)
	for index, existing := range rows {
		if !existing.EventTime.Before(cutoff) {
			firstRecent = index
			break
		}
	}
	last32 := len(rows) - 32
	if last32 < 0 {
		last32 = 0
	}
	start := firstRecent
	if last32 < start {
		start = last32
	}
	p.history[trID] = append([]mlclient.TelemetryPoint(nil), rows[start:]...)
}

func finitePointer(value float64) *float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return nil
	}
	result := value
	return &result
}

func selectTarget(events []catalog.ScheduleEvent, forecastTime time.Time) (catalog.ScheduleEvent, bool) {
	lower, upper := forecastTime.Add(10*time.Minute), forecastTime.Add(15*time.Minute)
	for _, event := range events {
		if event.PlannedAt.After(lower) && !event.PlannedAt.After(upper) {
			return event, true
		}
	}
	return catalog.ScheduleEvent{}, false
}

func (p *Processor) flush(parent context.Context) {
	p.mu.Lock()
	if len(p.pending) == 0 {
		p.mu.Unlock()
		return
	}
	candidates := make([]predictionCandidate, 0, len(p.pending))
	for unitID, candidate := range p.pending {
		candidates = append(candidates, candidate)
		delete(p.pending, unitID)
	}
	p.mu.Unlock()

	points := make([]mlclient.PredictionPoint, 0, len(candidates))
	telemetry := make([]mlclient.TelemetryPoint, 0)
	for _, candidate := range candidates {
		points = append(points, candidate.point)
		for _, row := range candidate.telemetry {
			if !row.EventTime.After(candidate.point.PredictionTime) {
				telemetry = append(telemetry, row)
			}
		}
	}
	ctx, cancel := context.WithTimeout(parent, p.predictionTimeout)
	results, err := p.predictor.PredictBatch(ctx, points, telemetry)
	cancel()
	if err != nil {
		p.logger.Warn("ML-прогноз временно недоступен", "error", err, "points", len(points))
		return
	}
	for _, candidate := range candidates {
		value, ok := results[candidate.point.SampleID]
		if !ok {
			p.logger.Warn("ML не вернул sample_id", "sample_id", candidate.point.SampleID)
			continue
		}
		prediction := models.DelayPrediction{
			ID: candidate.point.SampleID, UnitID: candidate.unitID, TRID: candidate.point.TRID,
			RoutePatternID: candidate.routePatternID, OccurrenceID: candidate.occurrenceID, TargetActionItemID: candidate.point.TargetStopID,
			TargetStop: candidate.targetStop, PredictionTime: candidate.point.PredictionTime,
			TargetPlannedAt: candidate.point.TargetTime, PredictedDelaySeconds: value,
		}
		currentDelay := candidate.point.CurrentDelay
		prediction.CurrentDelaySeconds = &currentDelay
		if err := p.runtime.ApplyPrediction(prediction, p.incidentThreshold, time.Now()); err != nil {
			p.logger.Error("не удалось сохранить инцидент", "error", err)
		}
	}
}
