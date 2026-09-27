package dashboard

import (
	"testing"
	"time"

	"mt_predictor/catalog"
	"mt_predictor/models"
)

func TestSnapshotMarksOldVehicleStale(t *testing.T) {
	runtime := &Runtime{
		ttl: 30 * time.Second, vehicles: map[uint32]models.VehicleState{
			10: {UnitID: 10, ReceivedAt: time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC), Freshness: models.TelemetryLive},
		}, subscribers: make(map[uint64]chan models.LiveEvent), matcher: catalog.NewMatcher(nil),
	}
	snapshot := runtime.Snapshot(time.Date(2026, 1, 1, 9, 1, 0, 0, time.UTC))
	if len(snapshot.Vehicles) != 1 || snapshot.Vehicles[0].Freshness != models.TelemetryStale {
		t.Fatalf("ожидался stale-маркер: %+v", snapshot)
	}
}

func TestSetVehicleDelayUpdatesPublishedVehicle(t *testing.T) {
	runtime := &Runtime{
		vehicles:    map[uint32]models.VehicleState{1099984: {UnitID: 1099984}},
		subscribers: make(map[uint64]chan models.LiveEvent),
	}
	if ok := runtime.SetVehicleDelay(1099984, 150, time.Now()); !ok {
		t.Fatal("existing vehicle was not updated")
	}
	vehicle, ok := runtime.Vehicle(1099984)
	if !ok || vehicle.CurrentDelaySeconds == nil || *vehicle.CurrentDelaySeconds != 150 {
		t.Fatalf("vehicle delay was not stored: %+v", vehicle)
	}
	runtime.PublishVehicle(models.VehicleState{UnitID: 1099984, EventTime: time.Now()}, time.Now())
	vehicle, _ = runtime.Vehicle(1099984)
	if vehicle.CurrentDelaySeconds == nil || *vehicle.CurrentDelaySeconds != 150 {
		t.Fatalf("telemetry update erased known delay: %+v", vehicle)
	}
}
