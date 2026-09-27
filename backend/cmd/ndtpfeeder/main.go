// Command ndtpfeeder replays a decoded telemetry CSV (traffic.csv) back onto
// the wire as NDTP packets.
//
// The Docker emulator only produces random telemetry around Moscow, which is
// enough to prove the receiver works but not enough to exercise a real
// trajectory. The feeder closes that gap without a second ingestion path: it
// speaks the same protocol over the same TCP socket as the emulator, so the
// backend cannot tell the difference.
//
// Timestamps in the dataset are naive Moscow wall clock. The feeder re-bases
// them onto the current time, preserving the gaps between consecutive points,
// so a replayed stream looks like a live one to the receiver.
//
// Usage:
//
//	ndtpfeeder -addr 127.0.0.1:9201 -file ../../dataset/validate/traffic.csv
//	ndtpfeeder -addr 127.0.0.1:9201 -file traffic.csv -speed 120 -vehicles 11
package main

import (
	"context"
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"mt_predictor/internal/ndtp"
)

func main() {
	var (
		addr          = flag.String("addr", "127.0.0.1:9201", "NDTP receiver address")
		file          = flag.String("file", "", "path to traffic.csv (required)")
		speed         = flag.Float64("speed", 1, "replay rate multiplier; 0 sends as fast as possible")
		vehicles      = flag.Int("vehicles", 11, "how many vehicles to replay; 0 replays every vehicle with a real trajectory")
		limit         = flag.Int("limit", 0, "stop after this many points per vehicle; 0 replays everything")
		loop          = flag.Bool("loop", false, "restart the replay when it ends")
		holdLast      = flag.Bool("hold-last", false, "keep sending the last point to preserve live freshness")
		timestampMode = flag.String("timestamp-mode", "rebased", "device timestamp: rebased or source")
		verbose       = flag.Bool("v", false, "log every point")
	)
	flag.Parse()

	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	if *file == "" {
		logger.Error("-file is required")
		os.Exit(2)
	}
	if err := run(runConfig{
		Addr:          *addr,
		File:          *file,
		Speed:         *speed,
		Vehicles:      *vehicles,
		Limit:         *limit,
		Loop:          *loop,
		HoldLast:      *holdLast,
		TimestampMode: *timestampMode,
		Logger:        logger,
	}); err != nil {
		logger.Error("feeder stopped", "error", err)
		os.Exit(1)
	}
}

type runConfig struct {
	Addr          string
	File          string
	Speed         float64
	Vehicles      int
	Limit         int
	Loop          bool
	HoldLast      bool
	TimestampMode string
	Logger        *slog.Logger
}

// signalContext cancels on Ctrl-C so a long replay stops cleanly.
func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

// sample is one CSV telemetry row, ready to be encoded.
type sample struct {
	// offset is the delay relative to the first point of the vehicle.
	offset time.Duration
	// sourceTime сохраняет event_time CSV для строгого сопоставления с расписанием.
	sourceTime time.Time
	nav        ndtp.NavCell
}

func run(cfg runConfig) error {
	if cfg.TimestampMode == "" {
		cfg.TimestampMode = "rebased"
	}
	if cfg.TimestampMode != "rebased" && cfg.TimestampMode != "source" {
		return fmt.Errorf("unknown timestamp mode %q", cfg.TimestampMode)
	}
	if cfg.TimestampMode == "source" && cfg.Loop {
		return errors.New("timestamp-mode=source cannot be combined with loop: event_time must stay monotonic")
	}
	if cfg.HoldLast && (cfg.Loop || cfg.TimestampMode != "source") {
		return errors.New("hold-last requires timestamp-mode=source and cannot be combined with loop")
	}
	tracks, err := loadTracks(cfg.File, cfg.Vehicles, cfg.Limit, cfg.Logger)
	if err != nil {
		return err
	}
	if len(tracks) == 0 {
		return errors.New("no telemetry rows to replay")
	}
	total := 0
	for _, track := range tracks {
		total += len(track)
	}
	cfg.Logger.Info("loaded telemetry",
		"file", cfg.File, "vehicles", len(tracks), "points", total,
		"speed", cfg.Speed, "loop", cfg.Loop, "timestamp_mode", cfg.TimestampMode)

	ctx, stop := signalContext()
	defer stop()

	for {
		if err := replay(ctx, cfg, tracks); err != nil {
			return err
		}
		if !cfg.Loop || ctx.Err() != nil {
			return nil
		}
		cfg.Logger.Info("replay finished, looping")
	}
}

// replay opens one connection per vehicle, exactly like the emulator does, and
// paces every vehicle against the same wall clock.
func replay(ctx context.Context, cfg runConfig, tracks map[uint32][]sample) error {
	units := make([]uint32, 0, len(tracks))
	for unit := range tracks {
		units = append(units, unit)
	}
	sort.Slice(units, func(i, j int) bool { return units[i] < units[j] })

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	start := time.Now()
	var wg sync.WaitGroup
	errCh := make(chan error, len(units))

	for _, unit := range units {
		wg.Add(1)
		go func(unit uint32, track []sample) {
			defer wg.Done()
			if err := feedVehicle(ctx, cfg, start, unit, track); err != nil && ctx.Err() == nil {
				errCh <- err
				cancel()
			}
		}(unit, tracks[unit])
	}

	wg.Wait()
	close(errCh)
	return <-errCh
}

// feedVehicle handshakes and then streams one vehicle's track.
func feedVehicle(ctx context.Context, cfg runConfig, start time.Time, unit uint32, track []sample) error {
	conn, err := net.DialTimeout("tcp", cfg.Addr, 5*time.Second)
	if err != nil {
		return fmt.Errorf("dial %s: %w", cfg.Addr, err)
	}
	defer conn.Close()

	// The emulator handshakes, waits, then streams realtime packets.
	handshake := ndtp.BuildFrame(unit, ndtp.ServiceGenericControls, ndtp.MsgConnRequest, 1,
		ndtp.BuildHandshakeBody(unit))
	refreshWriteDeadline(ctx, conn)
	if _, err := conn.Write(handshake); err != nil {
		return fmt.Errorf("vehicle %d handshake: %w", unit, err)
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(200 * time.Millisecond):
	}

	requestID := uint32(1)
	sendSample := func(s sample, sentAt time.Time) error {
		requestID++
		nav := s.nav
		if cfg.TimestampMode == "source" {
			// ML-demo должен оставаться в календаре исторического расписания.
			nav.Timestamp = s.sourceTime
		} else {
			// Обычный replay выглядит как текущий live-поток.
			nav.Timestamp = sentAt
		}
		body := ndtp.AppendCell(nil, ndtp.CellNav00, 0, ndtp.EncodeNavCell(nav))
		body = ndtp.AppendCell(body, ndtp.CellUsi08, 0, make([]byte, 6))
		frame := ndtp.BuildFrame(unit, ndtp.ServiceNavData, ndtp.MsgRealtime, requestID, body)
		refreshWriteDeadline(ctx, conn)
		if _, err := conn.Write(frame); err != nil {
			return fmt.Errorf("vehicle %d write: %w", unit, err)
		}
		cfg.Logger.Debug("sent point", "vehicle", unit, "speed", nav.SpeedAvg, "valid", nav.Flags.Valid)
		return nil
	}
	for _, s := range track {
		sentAt, err := waitUntil(ctx, start, s.offset, cfg.Speed)
		if err != nil {
			return err
		}
		if err := sendSample(s, sentAt); err != nil {
			return err
		}
	}
	if cfg.HoldLast && len(track) > 0 {
		cfg.Logger.Info("holding last telemetry point", "vehicle", unit, "interval", 5*time.Second)
		for {
			timer := time.NewTimer(5 * time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
			if err := sendSample(track[len(track)-1], time.Now()); err != nil {
				return err
			}
		}
	}
	cfg.Logger.Info("vehicle replayed", "vehicle", unit, "points", len(track))
	return nil
}

// refreshWriteDeadline ограничивает одну TCP-запись, а не весь replay.
// При масштабе 1x одно соединение живёт часами, поэтому единый deadline,
// установленный при подключении, ошибочно обрывал бы его через 30 секунд.
func refreshWriteDeadline(ctx context.Context, conn net.Conn) {
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetWriteDeadline(deadline)
		return
	}
	_ = conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
}

// replayInstant maps a dataset offset onto the simulated live clock.
//
// The device timestamp has to advance with the replayed timeline. Stamping every
// point with time.Now() collapses a whole track onto a single instant whenever
// the replay is compressed, and the receiver then cannot tell consecutive points
// apart, which matters because both the anti-leakage window and the schedule
// lookup key off the device clock.
func replayInstant(start time.Time, offset time.Duration, speed float64) time.Time {
	if speed <= 0 {
		// Unpaced: points go out as fast as the socket accepts them, so
		// simulated time barely advances and the wall clock is the best stamp.
		return time.Now()
	}
	// speed is a plain multiplier, so 60 means sixty times faster than real time
	// and a two minute gap becomes two seconds.
	//
	// The division has to happen in floating point. Dividing one Duration by
	// another is integer division and yields a bare count, which as a Duration
	// would be nanoseconds: 2s/60 would come out as 0ns and every point would be
	// scheduled at the start of the replay.
	return start.Add(time.Duration(float64(offset) / speed))
}

// waitUntil blocks until the sample's scheduled send time and returns that
// instant, which becomes the point's device timestamp.
func waitUntil(ctx context.Context, start time.Time, offset time.Duration, speed float64) (time.Time, error) {
	if speed <= 0 {
		return time.Now(), ctx.Err()
	}
	target := replayInstant(start, offset, speed)
	delay := time.Until(target)
	if delay <= 0 {
		return target, nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return time.Time{}, ctx.Err()
	case <-timer.C:
		return target, nil
	}
}

// loadTracks reads traffic.csv and groups it per unit, ordered by time.
func loadTracks(path string, wantVehicles, limit int, logger *slog.Logger) (map[uint32][]sample, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.ReuseRecord = true
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}
	columns := indexColumns(header)

	// unitId -> raw rows. Collected first because the CSV interleaves vehicles.
	rowsByUnit := make(map[uint32][]row)
	seen := 0

	for {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		unit, err := parseUint32(columns, record, "unit_id")
		if err != nil {
			return nil, err
		}
		at, err := parseTime(columns, record, "event_time")
		if err != nil {
			return nil, err
		}
		nav, err := parseNav(columns, record)
		if err != nil {
			return nil, err
		}
		rowsByUnit[unit] = append(rowsByUnit[unit], row{at: at, nav: nav})
		seen++
	}
	if seen == 0 {
		return nil, fmt.Errorf("%s has no data rows", path)
	}

	units, skipped := pickVehicles(rowsByUnit, wantVehicles)
	logger.Info("selected vehicles",
		"replaying", len(units), "skipped_no_trajectory", skipped,
		"skipped_because_requested", max(0, len(rowsByUnit)-len(skipped)-len(units)))

	tracks := make(map[uint32][]sample, len(units))
	for _, unit := range units {
		rows := rowsByUnit[unit]
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].at.Before(rows[j].at) })
		if limit > 0 && len(rows) > limit {
			rows = rows[:limit]
		}
		origin := rows[0].at
		track := make([]sample, 0, len(rows))
		for _, r := range rows {
			track = append(track, sample{offset: r.at.Sub(origin), sourceTime: r.at, nav: r.nav})
		}
		tracks[unit] = track
	}
	return tracks, nil
}

// minTrackPoints is the smallest number of points worth replaying. The dataset
// contains vehicles with a single row, which have no trajectory at all: there is
// nothing to interpolate, nothing to predict ahead of, and nothing to show on a
// map.
const minTrackPoints = 2

// pickVehicles chooses which units to replay, richest track first.
//
// Ordering by point count rather than by ID matters: the dataset's seven
// one-row vehicles (663271, 664030, 668372, 794446, 890371, 1112060, 1120670)
// all sort below the real fleet, so picking the lowest IDs yields vehicles that
// emit a single packet and vanish. Ordering is total, so the selection is
// reproducible.
func pickVehicles(rowsByUnit map[uint32][]row, want int) (units, skipped []uint32) {
	type ranked struct {
		unit   uint32
		points int
	}
	candidates := make([]ranked, 0, len(rowsByUnit))
	for unit, rows := range rowsByUnit {
		if len(rows) < minTrackPoints {
			skipped = append(skipped, unit)
			continue
		}
		candidates = append(candidates, ranked{unit: unit, points: len(rows)})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].points != candidates[j].points {
			return candidates[i].points > candidates[j].points
		}
		return candidates[i].unit < candidates[j].unit
	})
	sort.Slice(skipped, func(i, j int) bool { return skipped[i] < skipped[j] })

	if want > 0 && len(candidates) > want {
		candidates = candidates[:want]
	}
	units = make([]uint32, 0, len(candidates))
	for _, c := range candidates {
		units = append(units, c.unit)
	}
	return units, skipped
}

// row is one raw CSV telemetry row, before it is ordered into a track.
type row struct {
	at  time.Time
	nav ndtp.NavCell
}

func indexColumns(header []string) map[string]int {
	columns := make(map[string]int, len(header))
	for i, name := range header {
		columns[strings.TrimSpace(name)] = i
	}
	return columns
}

func field(columns map[string]int, record []string, name string) (string, error) {
	i, ok := columns[name]
	if !ok {
		return "", fmt.Errorf("column %q not found", name)
	}
	if i >= len(record) {
		return "", fmt.Errorf("column %q is out of range for a %d column row", name, len(record))
	}
	return strings.TrimSpace(record[i]), nil
}

func parseUint32(columns map[string]int, record []string, name string) (uint32, error) {
	raw, err := field(columns, record, name)
	if err != nil {
		return 0, err
	}
	if raw == "" {
		return 0, fmt.Errorf("column %q is empty", name)
	}
	value, err := strconv.ParseUint(raw, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("column %q: %w", name, err)
	}
	return uint32(value), nil
}

// eventTimeLayout accepts both the nanosecond and the whole-second forms found
// in the dataset.
const eventTimeLayout = "2006-01-02 15:04:05.999999999"

func parseTime(columns map[string]int, record []string, name string) (time.Time, error) {
	raw, err := field(columns, record, name)
	if err != nil {
		return time.Time{}, err
	}
	// The dataset stores naive Moscow wall clock. Only differences between
	// timestamps matter here, so any consistent zone works and UTC avoids
	// depending on a tz database being installed.
	at, err := time.ParseInLocation(eventTimeLayout, raw, time.UTC)
	if err != nil {
		return time.Time{}, fmt.Errorf("column %q value %q: %w", name, raw, err)
	}
	return at, nil
}

func parseNav(columns map[string]int, record []string) (ndtp.NavCell, error) {
	var nav ndtp.NavCell

	valid, err := parseBool(columns, record, "location_valid")
	if err != nil {
		return nav, err
	}
	lon, lonOK, err := parseFloat(columns, record, "lon")
	if err != nil {
		return nav, err
	}
	lat, latOK, err := parseFloat(columns, record, "lat")
	if err != nil {
		return nav, err
	}
	speed, _, err := parseFloat(columns, record, "speed")
	if err != nil {
		return nav, err
	}
	heading, _, err := parseFloat(columns, record, "heading")
	if err != nil {
		return nav, err
	}
	alt, _, err := parseFloat(columns, record, "alt")
	if err != nil {
		return nav, err
	}

	// A row is only a position when the dataset says the fix is valid. Rows
	// with location_valid=False often still carry stale or zeroed coordinates,
	// and sending them as valid would poison the map matching downstream.
	positioned := valid && lonOK && latOK
	if positioned {
		nav.Longitude, nav.Latitude = lon, lat
		nav.Flags.North, nav.Flags.East = lat >= 0, lon >= 0
	}
	nav.Flags.Valid = positioned
	nav.SpeedAvg = speed
	nav.SpeedMax = speed
	nav.Course = heading
	nav.Altitude = clampAltitude(alt)
	nav.Satellites = 8
	nav.PDOP = 2
	nav.BatteryVoltage = 4.2
	return nav, nil
}

// clampAltitude drops the dataset's sentinel values.
//
// The alt column reaches 65505 m, which is the 16-bit field wrapping, not an
// altitude. Moscow sits near 150 m, so anything beyond a few kilometres is
// treated as absent.
func clampAltitude(alt float64) float64 {
	const maxPlausible = 3000
	if math.IsNaN(alt) || alt < -500 || alt > maxPlausible {
		return 0
	}
	return alt
}

func parseBool(columns map[string]int, record []string, name string) (bool, error) {
	raw, err := field(columns, record, name)
	if err != nil {
		return false, err
	}
	if raw == "" {
		return false, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("column %q value %q: %w", name, raw, err)
	}
	return value, nil
}

func parseFloat(columns map[string]int, record []string, name string) (value float64, ok bool, err error) {
	raw, err := field(columns, record, name)
	if err != nil {
		return 0, false, err
	}
	if raw == "" {
		return 0, false, nil
	}
	value, err = strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, false, fmt.Errorf("column %q value %q: %w", name, raw, err)
	}
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, false, nil
	}
	return value, true, nil
}
