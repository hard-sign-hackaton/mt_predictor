package dashboard

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"mt_predictor/catalog"
	"mt_predictor/internal/ndtp"
	"mt_predictor/models"
)

// Runtime хранит согласованное изменяемое состояние карты, прогнозов и
// активных инцидентов. Это состояние процесса, а не постоянное хранилище.
type Runtime struct {
	mu          sync.RWMutex
	matcher     *catalog.Matcher
	ttl         time.Duration
	version     uint64
	vehicles    map[uint32]models.VehicleState
	predictions map[string]models.DelayPrediction
	incidents   map[string]models.Incident
	subscribers map[uint64]chan models.LiveEvent
	nextSubID   uint64
}

func NewRuntime(matcher *catalog.Matcher, ttl time.Duration) *Runtime {
	return &Runtime{
		matcher: matcher, ttl: ttl,
		vehicles: make(map[uint32]models.VehicleState), predictions: make(map[string]models.DelayPrediction),
		incidents: make(map[string]models.Incident), subscribers: make(map[uint64]chan models.LiveEvent),
	}
}

// Apply оставлен для простых пользователей runtime. Полный online-контур
// использует Processor, который дополнительно рассчитывает задержку и ML-цель.
func (r *Runtime) Apply(point ndtp.TelemetryPoint) models.VehicleState {
	lon, lat, valid := point.Nav.Position()
	telemetry := catalog.Telemetry{
		UnitID: point.VehicleID, Lon: lon, Lat: lat,
		Speed: point.Nav.SpeedAvg, Heading: point.Nav.Course,
		EventTime: point.Nav.Timestamp, ReceiveTime: point.ReceivedAt, Valid: valid,
	}
	match := r.matcher.MatchLive(telemetry)
	vehicle := catalog.VehicleState(telemetry, match, models.TelemetryLive)
	current, _ := r.PublishVehicle(vehicle, point.ReceivedAt)
	return current
}

// PublishVehicle атомарно применяет новое состояние и не разрешает запоздалому
// пакету откатить маркер. bool=false означает, что сохранено более новое состояние.
func (r *Runtime) PublishVehicle(vehicle models.VehicleState, occurredAt time.Time) (models.VehicleState, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, exists := r.vehicles[vehicle.UnitID]
	if exists && vehicle.EventTime.Before(current.EventTime) {
		return current, false
	}
	r.vehicles[vehicle.UnitID] = vehicle
	r.publishLocked(models.LiveEvent{
		Type: models.LiveVehicleUpdated, OccurredAt: occurredAt, Vehicle: &vehicle,
	}, fmt.Sprintf("vehicle-%d", vehicle.UnitID))
	return vehicle, true
}

// Vehicle возвращает последнюю сохранённую копию состояния ТС.
func (r *Runtime) Vehicle(unitID uint32) (models.VehicleState, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	vehicle, ok := r.vehicles[unitID]
	return vehicle, ok
}

// ApplyPrediction публикует прогноз и синхронно поддерживает минимальную
// очередь инцидентов. Критичность остаётся frontend-представлением порогов.
func (r *Runtime) ApplyPrediction(prediction models.DelayPrediction, incidentThreshold float64, now time.Time) {
	key := targetKey(prediction.UnitID, prediction.TargetActionItemID)
	r.mu.Lock()
	defer r.mu.Unlock()
	for existingKey, existing := range r.predictions {
		if existing.UnitID == prediction.UnitID && existingKey != key {
			delete(r.predictions, existingKey)
		}
	}
	r.predictions[key] = prediction
	r.publishLocked(models.LiveEvent{
		Type: models.LivePredictionUpdated, OccurredAt: now, Prediction: &prediction,
	}, "prediction-"+prediction.ID)

	existing, active := r.incidents[key]
	if prediction.PredictedDelaySeconds > incidentThreshold {
		eventType := models.IncidentNew
		createdAt := now
		if active {
			eventType = models.IncidentUpdated
			createdAt = existing.CreatedAt
		}
		incident := models.Incident{
			ID:        fmt.Sprintf("incident-%d-%d", prediction.UnitID, prediction.TargetActionItemID),
			EventType: eventType, UnitID: prediction.UnitID, TRID: prediction.TRID,
			RoutePatternID: prediction.RoutePatternID, PredictionID: prediction.ID,
			TargetActionItemID: prediction.TargetActionItemID, TargetStop: prediction.TargetStop,
			PredictedDelaySeconds: prediction.PredictedDelaySeconds, PredictionTime: prediction.PredictionTime,
			CreatedAt: createdAt, UpdatedAt: now,
		}
		r.incidents[key] = incident
		r.publishLocked(models.LiveEvent{
			Type: models.LiveIncidentUpdated, OccurredAt: now, Incident: &incident,
		}, "incident-"+incident.ID)
		return
	}
	if active {
		r.closeIncidentLocked(key, existing, now)
	}
}

// CloseOtherTargets закрывает прежнюю цель ТС, когда окно 10–15 минут уже
// переключилось на следующую остановку или прежняя цель пройдена.
func (r *Runtime) CloseOtherTargets(unitID uint32, keepActionItemID int64, now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	keepKey := targetKey(unitID, keepActionItemID)
	for key, prediction := range r.predictions {
		if prediction.UnitID == unitID && key != keepKey {
			delete(r.predictions, key)
		}
	}
	for key, incident := range r.incidents {
		if incident.UnitID == unitID && key != keepKey {
			r.closeIncidentLocked(key, incident, now)
		}
	}
}

func (r *Runtime) closeIncidentLocked(key string, incident models.Incident, now time.Time) {
	delete(r.incidents, key)
	incident.EventType = models.IncidentClosed
	incident.UpdatedAt = now
	r.publishLocked(models.LiveEvent{
		Type: models.LiveIncidentUpdated, OccurredAt: now, Incident: &incident,
	}, "incident-"+incident.ID)
}

func targetKey(unitID uint32, actionItemID int64) string {
	return fmt.Sprintf("%d:%d", unitID, actionItemID)
}

func (r *Runtime) publishLocked(event models.LiveEvent, prefix string) {
	r.version++
	event.Sequence = r.version
	event.EventID = fmt.Sprintf("%s-%d", prefix, event.Sequence)
	for _, subscriber := range r.subscribers {
		select {
		case subscriber <- event:
		default:
			// Клиент обнаружит разрыв sequence и запросит новый snapshot.
		}
	}
}

func (r *Runtime) snapshotVehiclesLocked(now time.Time) []models.VehicleState {
	vehicles := make([]models.VehicleState, 0, len(r.vehicles))
	for _, vehicle := range r.vehicles {
		if r.ttl > 0 && now.Sub(vehicle.ReceivedAt) > r.ttl {
			vehicle.Freshness = models.TelemetryStale
		}
		vehicles = append(vehicles, vehicle)
	}
	sort.Slice(vehicles, func(i, j int) bool { return vehicles[i].UnitID < vehicles[j].UnitID })
	return vehicles
}

func (r *Runtime) Snapshot(now time.Time) models.MapSnapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return models.MapSnapshot{Version: r.version, CreatedAt: now, Vehicles: r.snapshotVehiclesLocked(now)}
}

func (r *Runtime) DashboardSnapshot(now time.Time) models.DashboardSnapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	predictions := make([]models.DelayPrediction, 0, len(r.predictions))
	for _, prediction := range r.predictions {
		predictions = append(predictions, prediction)
	}
	incidents := make([]models.Incident, 0, len(r.incidents))
	for _, incident := range r.incidents {
		incidents = append(incidents, incident)
	}
	sort.Slice(predictions, func(i, j int) bool {
		if predictions[i].UnitID != predictions[j].UnitID {
			return predictions[i].UnitID < predictions[j].UnitID
		}
		return predictions[i].TargetActionItemID < predictions[j].TargetActionItemID
	})
	sort.Slice(incidents, func(i, j int) bool {
		if incidents[i].PredictedDelaySeconds != incidents[j].PredictedDelaySeconds {
			return incidents[i].PredictedDelaySeconds > incidents[j].PredictedDelaySeconds
		}
		return incidents[i].ID < incidents[j].ID
	})
	return models.DashboardSnapshot{
		Version: r.version, CreatedAt: now, Vehicles: r.snapshotVehiclesLocked(now),
		Predictions: predictions, Incidents: incidents,
	}
}

func (r *Runtime) Subscribe() (<-chan models.LiveEvent, func()) {
	r.mu.Lock()
	r.nextSubID++
	id := r.nextSubID
	channel := make(chan models.LiveEvent, 64)
	r.subscribers[id] = channel
	r.mu.Unlock()
	return channel, func() {
		r.mu.Lock()
		delete(r.subscribers, id)
		r.mu.Unlock()
	}
}
