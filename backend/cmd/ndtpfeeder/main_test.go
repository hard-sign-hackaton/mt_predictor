package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"mt_predictor/internal/ndtp"
)

const csvHeader = "packet_id,tr_id,unit_id,event_time,device_event_id,location_valid,gps_time,lon,lat,alt,speed,heading,receive_time,is_hist_data"

func writeCSV(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "traffic.csv")
	body := csvHeader + "\n"
	for _, line := range lines {
		body += line + "\n"
	}
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// pointRow is a valid, positioned row for one unit at a given wall clock time.
func pointRow(unit int, clock string) string {
	return "p," + strconv.Itoa(unit) + "," + strconv.Itoa(unit) +
		",2026-01-06 " + clock + ",0,True,,37.6,55.7,150,24.0,180.0,,False"
}

func TestLoadTracksGroupsAndOrdersByUnit(t *testing.T) {
	path := writeCSV(t,
		// Deliberately out of order, and split across two units.
		"p2,122048,893159,2026-01-06 12:30:45.000000000,0,True,,37.6173210,55.7551234,150,24.0,180.0,,False",
		"p1,122048,893159,2026-01-06 12:30:31.462764,0,True,,37.6170000,55.7550000,150,10.0,90.0,,False",
		"p3,130072,896671,2026-01-06 12:31:00,0,True,,37.6000000,55.7000000,150,30.0,270.0,,False",
		"p4,130072,896671,2026-01-06 12:30:30,0,True,,37.6000000,55.7000000,150,30.0,270.0,,False",
	)

	tracks, err := loadTracks(path, 0, 0, quietLogger())
	require.NoError(t, err)
	require.Len(t, tracks, 2)

	unit := tracks[893159]
	require.Len(t, unit, 2)
	assert.Equal(t, time.Duration(0), unit[0].offset, "earliest point is the origin")
	assert.Equal(t, 13*time.Second+537236*time.Microsecond, unit[1].offset,
		"12:30:45.000 - 12:30:31.462764")
	assert.InDelta(t, 10.0, unit[0].nav.SpeedAvg, 1e-9)
	assert.InDelta(t, 24.0, unit[1].nav.SpeedAvg, 1e-9)

	require.Len(t, tracks[896671], 2)
}

func TestLoadTracksHonoursValidFlag(t *testing.T) {
	path := writeCSV(t,
		// Valid fix: coordinates must survive.
		"p1,1,893159,2026-01-06 12:30:31,0,True,,37.6173210,55.7551234,150,24.0,180.0,,False",
		// Invalid fix that still carries a position: it must be dropped, the same
		// way traffic.csv's location_valid=False rows are unusable.
		"p2,1,893159,2026-01-06 12:30:40,0,False,,37.6173210,55.7551234,150,0.0,0.0,,False",
		// Invalid fix with no coordinates at all.
		"p3,1,893159,2026-01-06 12:30:50,0,False,,,,,,,,False",
	)

	tracks, err := loadTracks(path, 0, 0, quietLogger())
	require.NoError(t, err)
	unit := tracks[893159]
	require.Len(t, unit, 3)

	assert.True(t, unit[0].nav.Flags.Valid)
	assert.InDelta(t, 37.6173210, unit[0].nav.Longitude, 1e-7)
	assert.InDelta(t, 55.7551234, unit[0].nav.Latitude, 1e-7)
	assert.True(t, unit[0].nav.Flags.North)
	assert.True(t, unit[0].nav.Flags.East)

	assert.False(t, unit[1].nav.Flags.Valid, "stale coordinates must not be sent as a position")
	assert.Zero(t, unit[1].nav.Longitude)
	assert.Zero(t, unit[1].nav.Latitude)
	// Speed is still meaningful even when the fix is not.
	assert.InDelta(t, 0.0, unit[1].nav.SpeedAvg, 1e-9)

	assert.False(t, unit[2].nav.Flags.Valid)
	assert.Zero(t, unit[2].nav.Longitude)
}

func TestLoadTracksClampsSentinelAltitude(t *testing.T) {
	// 65505 m is the 16-bit field wrapping, not an altitude.
	path := writeCSV(t,
		"p1,1,893159,2026-01-06 12:30:31,0,True,,37.6,55.7,65505,24.0,180.0,,False",
		"p2,1,893159,2026-01-06 12:30:40,0,True,,37.6,55.7,156,24.0,180.0,,False",
		"p3,1,893159,2026-01-06 12:30:50,0,True,,37.6,55.7,-9999,24.0,180.0,,False",
	)
	tracks, err := loadTracks(path, 0, 0, quietLogger())
	require.NoError(t, err)
	unit := tracks[893159]
	require.Len(t, unit, 3)
	assert.Zero(t, unit[0].nav.Altitude)
	assert.InDelta(t, 156.0, unit[1].nav.Altitude, 1e-9)
	assert.Zero(t, unit[2].nav.Altitude)
}

// TestLoadTracksLimitsVehiclesAndPoints checks that -vehicles picks the richest
// tracks and -limit truncates each of them.
func TestLoadTracksLimitsVehiclesAndPoints(t *testing.T) {
	rows := []string{
		pointRow(100, "12:30:31"),
		pointRow(200, "12:30:31"),
		pointRow(300, "12:30:31"),
		pointRow(100, "12:30:40"),
		pointRow(100, "12:30:50"),
		pointRow(100, "12:31:00"),
		pointRow(100, "12:31:10"),
		pointRow(300, "12:30:40"),
		pointRow(300, "12:30:50"),
		pointRow(300, "12:31:00"),
	}
	tracks, err := loadTracks(writeCSV(t, rows...), 2, 2, quietLogger())
	require.NoError(t, err)

	require.Len(t, tracks, 2)
	assert.Contains(t, tracks, uint32(100), "five points beats four")
	assert.Contains(t, tracks, uint32(300), "four points beats one")
	assert.NotContains(t, tracks, uint32(200), "a single point is not a trajectory")
	assert.Len(t, tracks[100], 2, "per-vehicle point limit")
	assert.Len(t, tracks[300], 2, "per-vehicle point limit")
}

func TestLoadTracksSkipsVehiclesWithoutATrajectory(t *testing.T) {
	rows := []string{
		pointRow(700, "12:30:31"), // single point
		pointRow(800, "12:30:31"),
		pointRow(800, "12:30:40"),
		pointRow(900, "12:30:31"), // single point
	}
	tracks, err := loadTracks(writeCSV(t, rows...), 0, 0, quietLogger())
	require.NoError(t, err)

	require.Len(t, tracks, 1, "only the vehicle with two points is replayable")
	assert.Contains(t, tracks, uint32(800))
}

func TestLoadTracksReplaysEveryVehicleWhenUnlimited(t *testing.T) {
	rows := []string{
		pointRow(700, "12:30:31"),
		pointRow(800, "12:30:31"),
		pointRow(800, "12:30:40"),
	}
	tracks, err := loadTracks(writeCSV(t, rows...), 0, 0, quietLogger())
	require.NoError(t, err)
	assert.Len(t, tracks, 1, "-vehicles 0 means every replayable vehicle, not every unit")
}

func TestMockTelemetryFixtureIsMovingRouteSegment(t *testing.T) {
	path := filepath.Join("..", "..", "data", "mock_telemetry.csv")
	tracks, err := loadTracks(path, 0, 0, quietLogger())
	require.NoError(t, err)
	require.Len(t, tracks, 1)

	track := tracks[1099984]
	require.GreaterOrEqual(t, len(track), 50)
	assert.GreaterOrEqual(t, track[len(track)-1].offset, 14*time.Minute)
	for _, point := range track {
		assert.GreaterOrEqual(t, float64(point.nav.SpeedAvg), 10.0)
		assert.True(t, point.nav.Flags.Valid)
	}
	assert.True(t, track[len(track)-1].sourceTime.After(track[0].sourceTime))
	assert.LessOrEqual(t, track[len(track)-1].sourceTime,
		time.Date(2026, 1, 6, 6, 43, 17, 0, time.UTC))
}

func TestLoadTracksRejectsMissingColumns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.csv")
	require.NoError(t, os.WriteFile(path, []byte("packet_id,tr_id\n1,2\n"), 0o600))
	_, err := loadTracks(path, 0, 0, quietLogger())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unit_id")
}

func TestLoadTracksRejectsEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.csv")
	require.NoError(t, os.WriteFile(path, []byte(csvHeader+"\n"), 0o600))
	_, err := loadTracks(path, 0, 0, quietLogger())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no data rows")
}

func TestLoadTracksRejectsMissingFile(t *testing.T) {
	_, err := loadTracks(filepath.Join(t.TempDir(), "nope.csv"), 0, 0, quietLogger())
	require.Error(t, err)
}

// feederRun runs the feeder in the background and remembers its result.
//
// Completion is signalled by closing a channel rather than by sending on it, so
// several observers can wait on it. A buffered send would be consumed by
// whichever test helper happened to look first, and the second observer would
// then wait forever on a channel that had already been drained.
type feederRun struct {
	done chan struct{}
	err  error
}

func startFeeder(cfg runConfig) *feederRun {
	f := &feederRun{done: make(chan struct{})}
	go func() {
		f.err = run(cfg)
		close(f.done)
	}()
	return f
}

// wait asserts that the feeder returned cleanly.
func (f *feederRun) wait(t *testing.T, d time.Duration) {
	t.Helper()
	select {
	case <-f.done:
		require.NoError(t, f.err)
	case <-time.After(d):
		t.Fatal("feeder did not finish")
	}
}

// collectPoints drains exactly want points from the receiver.
//
// The feeder can finish writing and return before the receiver has emitted every
// point, so a clean return is not a failure and does not stop the drain: only a
// non-nil error does.
func collectPoints(t *testing.T, points <-chan ndtp.TelemetryPoint, f *feederRun,
	want int, stats func() ndtp.Stats) []ndtp.TelemetryPoint {
	t.Helper()
	got := make([]ndtp.TelemetryPoint, 0, want)
	done := f.done
	deadline := time.After(30 * time.Second)
	for len(got) < want {
		select {
		case p := <-points:
			got = append(got, p)
		case <-done:
			if f.err != nil {
				t.Fatalf("feeder failed after %d of %d points: %v", len(got), want, f.err)
			}
			done = nil // stop selecting on it, but keep draining
		case <-deadline:
			s := stats()
			t.Fatalf("timed out after %d of %d points: handshakes=%d points=%d crc_failed=%d nav_missing=%d cell_errors=%d",
				len(got), want, s.Handshakes, s.TelemetryPoints, s.CRCFailed,
				s.NavMissing, s.CellDecodeErrors)
		}
	}
	return got
}

// TestRunReplaysDatasetOverTheWire feeds CSV rows through the real encoder into
// a real receiver, so the whole NDTP path is exercised without Docker.
func TestRunReplaysDatasetOverTheWire(t *testing.T) {
	points := make(chan ndtp.TelemetryPoint, 512)
	receiver := ndtp.NewServer(ndtp.Options{
		Addr:        "127.0.0.1:0",
		OnTelemetry: func(p ndtp.TelemetryPoint) { points <- p },
		Logger:      quietLogger(),
	})
	require.NoError(t, receiver.Listen())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go receiver.Serve(ctx)
	defer receiver.Close()

	path := writeCSV(t,
		"p1,122048,893159,2026-01-06 12:30:31,0,True,,37.6173210,55.7551234,150,24.0,180.0,,False",
		"p2,122048,893159,2026-01-06 12:30:40,0,True,,37.6174000,55.7552000,150,26.0,180.0,,False",
		"p3,130072,896671,2026-01-06 12:30:31,0,True,,37.6000000,55.7000000,150,30.0,270.0,,False",
		"p4,130072,896671,2026-01-06 12:30:41,0,True,,37.6001000,55.7001000,150,31.0,270.0,,False",
		// location_valid=False rows must still be delivered: the vehicle exists
		// and is moving, only the fix is unusable.
		"p5,130072,896671,2026-01-06 12:30:51,0,False,,,,,0.0,0.0,,False",
	)

	feeder := startFeeder(runConfig{
		Addr:     receiver.Addr().String(),
		File:     path,
		Speed:    0, // no pacing
		Vehicles: 0,
		Logger:   quietLogger(),
	})

	got := collectPoints(t, points, feeder, 5, receiver.Stats)
	seen := map[uint32]int{}
	invalid := 0
	for _, p := range got {
		seen[p.VehicleID]++
		if !p.Nav.Flags.Valid {
			invalid++
		}
	}
	assert.Equal(t, 2, seen[893159], "vehicle 122048 arrives as unit 893159")
	assert.Equal(t, 3, seen[896671])
	assert.Equal(t, 1, invalid, "an unusable fix is still a telemetry point")

	feeder.wait(t, 10*time.Second)

	stats := receiver.Stats()
	assert.EqualValues(t, 2, stats.Handshakes, "one handshake per vehicle connection")
	assert.EqualValues(t, 5, stats.TelemetryPoints)
	assert.EqualValues(t, 0, stats.CRCFailed, "frames built by the encoder must validate")
	assert.EqualValues(t, 0, stats.CellDecodeErrors)
}

// TestRunReplaysRealDataset runs the feeder against the shipped dataset when it
// is present, which is the configuration used for demos.
func TestRunReplaysRealDataset(t *testing.T) {
	const dataset = "../../../../dataset/validate/traffic.csv"
	if _, err := os.Stat(dataset); err != nil {
		t.Skipf("dataset not available: %v", err)
	}

	points := make(chan ndtp.TelemetryPoint, 256)
	receiver := ndtp.NewServer(ndtp.Options{
		Addr:        "127.0.0.1:0",
		OnTelemetry: func(p ndtp.TelemetryPoint) { points <- p },
		Logger:      quietLogger(),
	})
	require.NoError(t, receiver.Listen())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go receiver.Serve(ctx)
	defer receiver.Close()

	const vehicles, perVehicle = 3, 20
	feeder := startFeeder(runConfig{
		Addr:     receiver.Addr().String(),
		File:     dataset,
		Speed:    0,
		Vehicles: vehicles,
		Limit:    perVehicle,
		Logger:   quietLogger(),
	})

	// The busiest vehicles must be chosen, never the one-row vehicles that sort
	// lowest by ID.
	got := collectPoints(t, points, feeder, vehicles*perVehicle, receiver.Stats)
	replayed := map[uint32]bool{}
	for _, p := range got {
		assert.False(t, p.Nav.Timestamp.IsZero(), "feeder must stamp a live time")
		assert.WithinDuration(t, time.Now(), p.Nav.Timestamp, time.Minute)
		replayed[p.VehicleID] = true
	}
	require.Len(t, replayed, vehicles, "one connection per vehicle")
	for _, dead := range []uint32{663271, 664030, 668372, 794446, 890371, 1112060, 1120670} {
		assert.False(t, replayed[dead],
			"vehicle %d has a single row and cannot be replayed", dead)
	}

	feeder.wait(t, 10*time.Second)

	stats := receiver.Stats()
	assert.EqualValues(t, vehicles, stats.Handshakes)
	assert.EqualValues(t, vehicles*perVehicle, stats.TelemetryPoints)
	assert.EqualValues(t, 0, stats.CRCFailed)
}

func TestWaitUntilRespectsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := waitUntil(ctx, time.Now(), time.Hour, 1)
	assert.ErrorIs(t, err, context.Canceled)

	// A speed of zero means no pacing, so an already-cancelled context is the
	// only way to stop.
	_, err = waitUntil(context.Background(), time.Now(), time.Hour, 0)
	assert.NoError(t, err)
}

func TestReplayInstantFollowsTheReplayedTimeline(t *testing.T) {
	start := time.Date(2026, 1, 6, 12, 0, 0, 0, time.UTC)

	// At 1x the device clock advances exactly like the dataset.
	assert.Equal(t, start.Add(2*time.Second), replayInstant(start, 2*time.Second, 1))
	// At 60x a two minute gap becomes two seconds, so the receiver sees a
	// plausible live stream rather than a burst. This is the case that integer
	// Duration division silently turned into zero.
	assert.Equal(t, start.Add(2*time.Second), replayInstant(start, 2*time.Minute, 60))
	assert.Equal(t, start.Add(30*time.Second), replayInstant(start, time.Hour, 120))
	// The origin is always the start of the replay.
	assert.Equal(t, start, replayInstant(start, 0, 60))

	// Unpaced: the stamp is the wall clock, and it must still be usable.
	unpaced := replayInstant(start, time.Hour, 0)
	assert.WithinDuration(t, time.Now(), unpaced, time.Minute)
}

// TestRunStampsDeviceTimeOnTheReplayedTimeline is the regression test for a
// compressed replay stamping every point with time.Now(), which made the whole
// track look simultaneous to the receiver.
func TestRunStampsDeviceTimeOnTheReplayedTimeline(t *testing.T) {
	points := make(chan ndtp.TelemetryPoint, 64)
	receiver := ndtp.NewServer(ndtp.Options{
		Addr:        "127.0.0.1:0",
		OnTelemetry: func(p ndtp.TelemetryPoint) { points <- p },
		Logger:      quietLogger(),
	})
	require.NoError(t, receiver.Listen())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go receiver.Serve(ctx)
	defer receiver.Close()

	// Three points two seconds apart, replayed at 1x. The wire format carries the
	// device clock as Unix seconds, so the gaps have to be whole seconds.
	path := writeCSV(t,
		"p1,1,893159,2026-01-06 12:30:31,0,True,,37.6,55.7,150,24.0,180.0,,False",
		"p2,1,893159,2026-01-06 12:30:33,0,True,,37.6,55.7,150,24.0,180.0,,False",
		"p3,1,893159,2026-01-06 12:30:35,0,True,,37.6,55.7,150,24.0,180.0,,False",
	)

	feeder := startFeeder(runConfig{
		Addr:     receiver.Addr().String(),
		File:     path,
		Speed:    1,
		Vehicles: 0,
		Logger:   quietLogger(),
	})

	got := collectPoints(t, points, feeder, 3, receiver.Stats)
	feeder.wait(t, 10*time.Second)

	times := make([]time.Time, len(got))
	for i, p := range got {
		times[i] = p.Nav.Timestamp
	}
	assert.True(t, times[0].Before(times[1]) && times[1].Before(times[2]),
		"device time must advance: %s, %s, %s", times[0], times[1], times[2])
	assert.Equal(t, 2*time.Second, times[1].Sub(times[0]))
	assert.Equal(t, 2*time.Second, times[2].Sub(times[1]))
	assert.WithinDuration(t, time.Now(), times[0], 5*time.Second,
		"the replayed timeline is re-based onto the current clock")
}

func TestSourceTimestampModePreservesDatasetEventTime(t *testing.T) {
	points := make(chan ndtp.TelemetryPoint, 8)
	receiver := ndtp.NewServer(ndtp.Options{Addr: "127.0.0.1:0", OnTelemetry: func(p ndtp.TelemetryPoint) { points <- p }, Logger: quietLogger()})
	require.NoError(t, receiver.Listen())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go receiver.Serve(ctx)
	defer receiver.Close()

	path := writeCSV(t,
		"p1,1,893159,2026-01-06 12:30:31,0,True,,37.6,55.7,150,24.0,180.0,,False",
		"p2,1,893159,2026-01-06 12:30:33,0,True,,37.6,55.7,150,24.0,180.0,,False",
	)
	feeder := startFeeder(runConfig{Addr: receiver.Addr().String(), File: path, Speed: 0, Vehicles: 0, TimestampMode: "source", Logger: quietLogger()})
	got := collectPoints(t, points, feeder, 2, receiver.Stats)
	feeder.wait(t, 10*time.Second)
	assert.Equal(t, time.Date(2026, 1, 6, 12, 30, 31, 0, time.UTC), got[0].Nav.Timestamp)
	assert.Equal(t, time.Date(2026, 1, 6, 12, 30, 33, 0, time.UTC), got[1].Nav.Timestamp)
}

func TestSourceTimestampModeRejectsLoop(t *testing.T) {
	err := run(runConfig{TimestampMode: "source", Loop: true, Logger: quietLogger()})
	require.EqualError(t, err, "timestamp-mode=source cannot be combined with loop: event_time must stay monotonic")
}

func TestHoldLastRequiresSourceTimestampMode(t *testing.T) {
	err := run(runConfig{TimestampMode: "rebased", HoldLast: true, Logger: quietLogger()})
	require.EqualError(t, err, "hold-last requires timestamp-mode=source and cannot be combined with loop")
}

func TestReplayReportsDialFailure(t *testing.T) {
	// Port 1 on loopback refuses connections.
	err := replay(context.Background(), runConfig{
		Addr:   "127.0.0.1:1",
		Logger: quietLogger(),
	}, map[uint32][]sample{1: {{offset: 0, nav: ndtp.NavCell{}}}})
	require.Error(t, err)
	var netErr net.Error
	assert.ErrorAs(t, err, &netErr)
}
