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
