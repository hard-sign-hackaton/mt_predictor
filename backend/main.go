// Command mt-predictor-backend starts the NDTP receiver.
//
// The Docker emulator and the ndtpfeeder are both TCP clients: each terminal
// opens its own connection here and streams NDTP packets. This program is the
// server side. It decodes every frame and prints the navigation data, so the
// ingest path can be observed on its own before any scheduling or prediction is
// wired in.
//
//	go run . -addr :9201          # receive
//	make feed                     # replay the dataset into 127.0.0.1:9201
//
// Flags:
//
//	-addr    TCP address to receive NDTP packets on
//	-stats   how often to print receiver counters, 0 disables
//	-quiet   log only warnings, leaving the telemetry table as the output
//
// Telemetry is the only thing written to stdout, so the table can be piped.
// Columns are space aligned for readability, so filter by word rather than by
// guessing at field positions:
//
//	./mt-predictor-backend | grep -w 893159
//	./mt-predictor-backend | awk '$3 == 893159 {print $4, $5}'
//
// Counters, warnings and the Ctrl-C summary go to stderr. On shutdown the
// program prints what the session saw.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sort"
	"sync"
	"sync/atomic"
	"syscall"
	"text/tabwriter"
	"time"

	"mt_predictor/internal/ndtp"
)

func main() {
	var (
		addr       = flag.String("addr", ":9201", "TCP address to receive NDTP packets on")
		statsEvery = flag.Duration("stats", 10*time.Second, "how often to print receiver counters; 0 disables")
		quiet      = flag.Bool("quiet", false, "log only warnings, leaving telemetry lines as the output")
	)
	flag.Parse()

	level := slog.LevelInfo
	if *quiet {
		level = slog.LevelWarn
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	if err := run(*addr, *statsEvery, logger); err != nil {
		logger.Error("receiver stopped", "error", err)
		os.Exit(1)
	}
}

func run(addr string, statsEvery time.Duration, logger *slog.Logger) error {
	// The console is much slower than the wire, so decoded points are handed to
	// a printer goroutine through a queue. ndtp.Server calls OnTelemetry on the
	// connection's own goroutine and requires it not to block, because a stalled
	// callback would stall packet ingest for that terminal.
	queue := make(chan ndtp.TelemetryPoint, 4096)
	var dropped atomic.Int64
	vehicles := newVehicleRegistry()

	receiver := ndtp.NewServer(ndtp.Options{
		Addr: addr,
		OnTelemetry: func(p ndtp.TelemetryPoint) {
			vehicles.record(p)
			select {
			case queue <- p:
			default:
				// Terminals outrun the console. Count the loss rather than
				// stalling the read loop; a real backend stores instead of
				// printing, so this is only a limit of this debug view.
				dropped.Add(1)
			}
		},
		Logger: logger,
	})

	// Bind before logging so the address reported is the one actually bound,
	// including a port chosen by the kernel.
	if err := receiver.Listen(); err != nil {
		return err
	}
	logger.Info("ndtp receiver bound", "addr", receiver.Addr().String())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	out := newPrinter(os.Stdout, os.Stderr)
	printed := make(chan struct{})
	go func() {
		defer close(printed)
		out.serve(ctx, queue)
	}()

	if statsEvery > 0 {
		go reportStats(ctx, receiver, vehicles, &dropped, statsEvery, out)
	}

	serveErr := receiver.Serve(ctx)

	// Serve returns once the listener is closed. Closing the queue here would
	// race with an in-flight OnTelemetry call from a connection goroutine that
	// has not finished yet, so the printer is stopped by the context instead and
	// the channel is simply left for the garbage collector.
	out.line("shutting down")

	// Give the printer a moment to drain what is already queued.
	grace := time.NewTimer(2 * time.Second)
	select {
	case <-printed:
	case <-grace.C:
	}
	grace.Stop()

	// Close waits for the connection goroutines, so no further point can arrive
	// once it returns.
	if err := receiver.Close(); err != nil {
		logger.Warn("receiver close", "error", err)
	}

	out.flush()
	out.line("vehicles seen: %d: %s", vehicles.count(), vehicles.summary())
	printFinalStats(receiver, vehicles, &dropped, out)
	return serveErr
}

// printer serialises console output so that nothing interleaves mid-line.
//
// The two streams are kept apart on purpose: telemetry goes to stdout and
// nothing else does, so the table stays intact and can be piped into grep or awk.
// Counters, warnings and the shutdown summary go to stderr, where they cannot
// corrupt a data pipeline.
type printer struct {
	mu    sync.Mutex
	data  *tabwriter.Writer
	note_ *os.File
}

func newPrinter(data, notes *os.File) *printer {
	return &printer{
		data:  tabwriter.NewWriter(data, 0, 0, 2, ' ', 0),
		note_: notes,
	}
}

func (p *printer) serve(ctx context.Context, queue <-chan ndtp.TelemetryPoint) {
	header := true
	for {
		select {
		case point := <-queue:
			if header {
				p.header()
				header = false
			}
			p.point(point)
		case <-ctx.Done():
			// Drain whatever is already queued, then stop.
			for {
				select {
				case point := <-queue:
					if header {
						p.header()
						header = false
					}
					p.point(point)
				default:
					p.flush()
					return
				}
			}
		}
	}
}

func (p *printer) header() {
	p.mu.Lock()
	defer p.mu.Unlock()
	fmt.Fprintln(p.data, "device time\tvehicle\tlongitude\tlatitude\talt m\tspeed km/h\tcourse\tsats\tpdop\tbatt V\tfix\tcells")
}

func (p *printer) point(point ndtp.TelemetryPoint) {
	nav := point.Nav
	lon, lat := "-", "-"
	fix := "none"
	if l, a, ok := nav.Position(); ok {
		lon = fmt.Sprintf("%.6f", l)
		lat = fmt.Sprintf("%.6f", a)
		fix = "ok"
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	fmt.Fprintf(p.data, "%s\t%d\t%s\t%s\t%.0f\t%.1f\t%.0f\t%d\t%d\t%.2f\t%s\t%d\n",
		nav.Timestamp.Format("2006-01-02 15:04:05.000"),
		point.VehicleID,
		lon, lat,
		nav.Altitude,
		nav.SpeedAvg,
		nav.Course,
		nav.Satellites,
		nav.PDOP,
		nav.BatteryVoltage,
		fix,
		point.CellsTrailing,
	)
}

// line writes an operator-facing message. It goes to stderr and flushes the
// telemetry table first, so the two never appear interleaved on one terminal.
func (p *printer) line(format string, args ...any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.data.Flush()
	fmt.Fprintf(p.note_, format+"\n", args...)
}

func (p *printer) flush() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.data.Flush()
}

// reportStats prints receiver counters on a timer.
func reportStats(ctx context.Context, receiver *ndtp.Server, vehicles *vehicleRegistry,
	dropped *atomic.Int64, every time.Duration, out *printer) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			stats := receiver.Stats()
			out.line("stats: up=%s conns=%d vehicles=%d handshakes=%d frames=%d realtime=%d points=%d crc_failed=%d nav_missing=%d cell_errors=%d resyncs=%d bytes=%d",
				time.Since(stats.StartedAt).Truncate(time.Second),
				stats.ActiveConnections,
				vehicles.count(),
				stats.Handshakes,
				stats.FramesTotal,
				stats.RealtimeFrames,
				stats.TelemetryPoints,
				stats.CRCFailed,
				stats.NavMissing,
				stats.CellDecodeErrors,
				stats.Resyncs,
				stats.BytesRead,
			)
		case <-ctx.Done():
			return
		}
	}
}

func printFinalStats(receiver *ndtp.Server, vehicles *vehicleRegistry, dropped *atomic.Int64, out *printer) {
	stats := receiver.Stats()
	out.line("final: accepted=%d closed=%d handshakes=%d frames=%d points=%d crc_failed=%d nav_missing=%d cell_errors=%d resyncs=%d bytes=%d dropped_by_console=%d",
		stats.AcceptedConnections,
		stats.ClosedConnections,
		stats.Handshakes,
		stats.FramesTotal,
		stats.TelemetryPoints,
		stats.CRCFailed,
		stats.NavMissing,
		stats.CellDecodeErrors,
		stats.Resyncs,
		stats.BytesRead,
		dropped.Load(),
	)
	vehicles.each(func(id uint32, first, last time.Time, points int) {
		out.line("vehicle %d: points=%d first=%s last=%s",
			id, points,
			first.Format("15:04:05.000"),
			last.Format("15:04:05.000"),
		)
	})
}

// vehicleRegistry tracks which terminals are talking, so the console shows how
// many distinct units are live rather than just how many packets arrived.
type vehicleRegistry struct {
	mu       sync.Mutex
	vehicles map[uint32]*vehicleSeen
}

type vehicleSeen struct {
	first  time.Time
	last   time.Time
	points int
}

func newVehicleRegistry() *vehicleRegistry {
	return &vehicleRegistry{vehicles: make(map[uint32]*vehicleSeen)}
}

func (r *vehicleRegistry) record(point ndtp.TelemetryPoint) {
	r.mu.Lock()
	defer r.mu.Unlock()
	seen, ok := r.vehicles[point.VehicleID]
	if !ok {
		seen = &vehicleSeen{first: point.ReceivedAt}
		r.vehicles[point.VehicleID] = seen
	}
	seen.last = point.ReceivedAt
	seen.points++
}

func (r *vehicleRegistry) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.vehicles)
}

func (r *vehicleRegistry) each(fn func(id uint32, first, last time.Time, points int)) {
	r.mu.Lock()
	snapshot := make(map[uint32]vehicleSeen, len(r.vehicles))
	for id, seen := range r.vehicles {
		snapshot[id] = *seen
	}
	r.mu.Unlock()
	for id, seen := range snapshot {
		fn(id, seen.first, seen.last, seen.points)
	}
}

func (r *vehicleRegistry) summary() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	ids := make([]int, 0, len(r.vehicles))
	for id := range r.vehicles {
		ids = append(ids, int(id))
	}
	sort.Ints(ids)
	out := ""
	for i, id := range ids {
		if i > 0 {
			out += ", "
		}
		out += fmt.Sprint(id)
	}
	return out
}
