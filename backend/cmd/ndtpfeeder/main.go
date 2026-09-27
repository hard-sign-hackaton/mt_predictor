// Команда ndtpfeeder отправляет декодированную телеметрию из CSV (traffic.csv)
// обратно на провод пакетами NDTP.
//
// Docker-эмулятор выдаёт только случайную телеметрию вокруг Москвы: этого
// достаточно, чтобы убедиться в работе приёмника, но недостаточно, чтобы
// прогнать настоящую траекторию. Фидер закрывает этот пробел без второго пути
// приёма данных: он говорит тем же протоколом через тот же TCP-сокет, что и
// эмулятор, поэтому backend не может их отличить.
//
// Время в наборе данных записано как наивное московское. Фидер переносит его
// на текущее, сохраняя промежутки между соседними точками, чтобы
// воспроизведённый поток выглядел для приёмника как живой.
//
// Использование:
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
		speed         = flag.Float64("speed", 60, "replay rate multiplier; 0 sends as fast as possible")
		vehicles      = flag.Int("vehicles", 11, "how many vehicles to replay; 0 replays every vehicle with a real trajectory")
		limit         = flag.Int("limit", 0, "stop after this many points per vehicle; 0 replays everything")
		loop          = flag.Bool("loop", false, "restart the replay when it ends")
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
	TimestampMode string
	Logger        *slog.Logger
}

// signalContext отменяет контекст по Ctrl-C, чтобы длинный прогон завершался чисто.
func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

// sample is one CSV telemetry row, ready to be encoded.
type sample struct {
	// offset — задержка относительно первой точки данного транспортного средства.
	offset     time.Duration
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

// replay открывает по соединению на каждое ТС, как эмулятор, и
// привязывает все ТС к общим часам.
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

// feedVehicle здоровается, затем передаёт трек одного ТС.
func feedVehicle(ctx context.Context, cfg runConfig, start time.Time, unit uint32, track []sample) error {
	conn, err := net.DialTimeout("tcp", cfg.Addr, 5*time.Second)
	if err != nil {
		return fmt.Errorf("dial %s: %w", cfg.Addr, err)
	}
	defer conn.Close()

	// Эмулятор сначала здоровается, затем ждёт и передаёт realtime-пакеты.
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
	for _, s := range track {
		sentAt, err := waitUntil(ctx, start, s.offset, cfg.Speed)
		if err != nil {
			return err
		}
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
// Время на устройстве должно двигаться вместе с воспроизводимой шкалой. Если
// ставить каждой точке time.Now(), весь трек схлопнется в один момент
// при сжатом прогоне, и приёмник перестанет различать соседние точки, а это
// важно, потому что и окно против утечки, и поиск по расписанию
// опираются на часы устройства.
func replayInstant(start time.Time, offset time.Duration, speed float64) time.Time {
	if speed <= 0 {
		// Без пауз: точки уходят так быстро, как принимает сокет, поэтому
		// смоделированное время почти не идёт, и лучшая отметка — реальные часы.
		return time.Now()
	}
	// speed — простой множитель, то есть 60 означает в шестьдесят раз быстрее
	// реального времени, и двухминутный разрыв становится двумя секундами.
	//
	// Деление обязано идти в плавающей точке. Деление одного Duration на
	// другой даёт целое число: как Duration это были бы наносекунды,
	// то есть 2s/60 дало бы 0ns и каждая точка попала бы в начало прогона.
	return start.Add(time.Duration(float64(offset) / speed))
}

// waitUntil ждёт запланированное время отправки и возвращает этот самый
// момент, который становится отметкой времени точки на устройстве.
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

// loadTracks читает traffic.csv и группирует строки по ТС, упорядочивая по времени.
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

	// unitId -> исходные строки. Сначала собираются целиком, потому что CSV перемешивает ТС.
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

// minTrackPoints — наименьшее число точек, ради которого стоит запускать прогон.
// В наборе есть ТС с единственной строкой, у которых нет траектории вовсе:
// нечего интерполировать, нечего прогнозировать и нечего показывать
// на карте.
const minTrackPoints = 2

// pickVehicles выбирает ТС для прогона, начиная с самым длинными треками.
//
// Сортировка по числу точек, а не по ID, важна: семь
// ТС с одной строкой (663271, 664030, 668372, 794446, 890371, 1112060, 1120670)
// при сортировке оказываются ниже основного парка, поэтому выбор наименьших ID
// дал бы ТС, которые отправят один пакет и исчезнут. Сортировка полная, поэтому
// выбор воспроизводим.
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

// eventTimeLayout принимает и наносекундную, и целосекундную формы, встречающиеся
// в наборе данных.
const eventTimeLayout = "2006-01-02 15:04:05.999999999"

func parseTime(columns map[string]int, record []string, name string) (time.Time, error) {
	raw, err := field(columns, record, name)
	if err != nil {
		return time.Time{}, err
	}
	// Набор хранит наивное московское время. Здесь важны только разности между
	// метками времени, поэтому подойдёт любая согласованная зона, а UTC избавляет
	// от зависимости от установленной базы часовых поясов.
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

	// Строка является координатой только когда набор считает фикс достоверным.
	// У строк с location_valid=False координаты часто устаревшие или нулевые,
	// и отправка их как достоверных испортила бы сопоставление по карте.
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

// clampAltitude отбрасывает служебные значения из набора данных.
//
// Колонка alt доходит до 65505 м: это переполнение 16-битного поля, а не
// высота. Москва лежит около 150 м, поэтому всё, что дальше нескольких километров,
// считается отсутствующим.
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
