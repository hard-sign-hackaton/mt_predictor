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

// Runtime хранит последнее состояние ТС и раздаёт обновления подписчикам карты.
// Это состояние процесса, а не долговременное хранилище.
type Runtime struct {
	mu          sync.RWMutex
	matcher     *catalog.Matcher
	ttl         time.Duration
	version     uint64
	vehicles    map[uint32]models.VehicleState
	subscribers map[uint64]chan models.LiveEvent
	nextSubID   uint64
}

// NewRuntime создаёт live-состояние поверх подготовленного каталога маршрутов.
func NewRuntime(matcher *catalog.Matcher, ttl time.Duration) *Runtime {
	return &Runtime{
		matcher: matcher, ttl: ttl, vehicles: make(map[uint32]models.VehicleState),
		subscribers: make(map[uint64]chan models.LiveEvent),
	}
}

// Apply принимает результат настоящего NDTP-декодирования, выполняет map
// matching и публикует нормализованное состояние ТС.
func (r *Runtime) Apply(point ndtp.TelemetryPoint) models.VehicleState {
	lon, lat, valid := point.Nav.Position()
	telemetry := catalog.Telemetry{
		UnitID: point.VehicleID, Lon: lon, Lat: lat,
		Speed: point.Nav.SpeedAvg, Heading: point.Nav.Course,
		EventTime: point.Nav.Timestamp, ReceiveTime: point.ReceivedAt, Valid: valid,
	}
	match := r.matcher.MatchLive(telemetry)
	vehicle := catalog.VehicleState(telemetry, match, models.TelemetryLive)

	r.mu.Lock()
	current, exists := r.vehicles[vehicle.UnitID]
	if exists && vehicle.EventTime.Before(current.EventTime) {
		r.mu.Unlock()
		return current
	}
	r.version++
	sequence := r.version
	r.vehicles[vehicle.UnitID] = vehicle
	event := models.LiveEvent{
		EventID:  fmt.Sprintf("vehicle-%d-%d", vehicle.UnitID, sequence),
		Sequence: sequence, Type: models.LiveVehicleUpdated,
		OccurredAt: point.ReceivedAt, Vehicle: &vehicle,
	}
	for _, subscriber := range r.subscribers {
		select {
		case subscriber <- event:
		default:
			// Медленный клиент увидит разрыв Sequence и запросит новый snapshot.
		}
	}
	r.mu.Unlock()
	return vehicle
}

// Snapshot возвращает согласованный снимок, помечая давно не обновлявшиеся ТС
// как stale. Исходное время NDTP при этом не изменяется.
func (r *Runtime) Snapshot(now time.Time) models.MapSnapshot {
	r.mu.RLock()
	vehicles := make([]models.VehicleState, 0, len(r.vehicles))
	for _, vehicle := range r.vehicles {
		if r.ttl > 0 && now.Sub(vehicle.ReceivedAt) > r.ttl {
			vehicle.Freshness = models.TelemetryStale
		}
		vehicles = append(vehicles, vehicle)
	}
	version := r.version
	r.mu.RUnlock()
	sort.Slice(vehicles, func(i, j int) bool { return vehicles[i].UnitID < vehicles[j].UnitID })
	return models.MapSnapshot{Version: version, CreatedAt: now, Vehicles: vehicles}
}

// Subscribe регистрирует клиента live-потока. Сначала нужно подписаться, затем
// получить Snapshot: возможные параллельные события останутся в канале.
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
