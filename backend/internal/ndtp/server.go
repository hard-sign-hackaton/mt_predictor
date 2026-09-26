package ndtp

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// TelemetryPoint is one decoded navigation record taken from a realtime frame.
// It is the unit the receiver hands to the rest of the backend.
type TelemetryPoint struct {
	// VehicleID is the NDTP peer address, that is the terminal's unitId.
	//
	// The project maps tr_id to unit_id directly, so this is also the tr_id
	// used to look up the schedule and to call the ML service.
	VehicleID  uint32
	Nav        NavCell
	ReceivedAt time.Time
	// CellsTrailing is how many cells followed the navigation cell. It is
	// reported for observability only; the navigation cell is what the delay
	// model consumes.
	CellsTrailing int
}

// Options configures a Server.
type Options struct {
	// Addr is the TCP address to listen on, for example ":9201". The emulator
	// connects to this port.
	Addr string
	// OnTelemetry is called for every realtime frame that yields a navigation
	// cell. It runs on the connection's goroutine and must not block.
	OnTelemetry func(TelemetryPoint)
	// Logger receives connection lifecycle and decode warnings. Defaults to
	// slog.Default().
	Logger *slog.Logger
	// IdleTimeout closes a connection that has sent nothing for this long.
	// Zero disables the deadline.
	IdleTimeout time.Duration
	// WriteTimeout bounds the best-effort handshake reply. Defaults to 5s.
	WriteTimeout time.Duration
}

const (
	defaultIdleTimeout  = 5 * time.Minute
	defaultWriteTimeout = 5 * time.Second
)

// Stats is a snapshot of receiver counters.
type Stats struct {
	AcceptedConnections int64
	ClosedConnections   int64
	ActiveConnections   int64
	Handshakes          int64
	RealtimeFrames      int64
	FramesTotal         int64
	CRCFailed           int64
	CellDecodeErrors    int64
	TelemetryPoints     int64
	NavMissing          int64
	PeerMismatch        int64
	BytesRead           int64
	Resyncs             int64
	StartedAt           time.Time
}

type counters struct {
	accepted  atomic.Int64
	closed    atomic.Int64
	handshake atomic.Int64
	realtime  atomic.Int64
	frames    atomic.Int64
	crcFailed atomic.Int64
	cellError atomic.Int64
	telemetry atomic.Int64
	navMissed atomic.Int64
	mismatch  atomic.Int64
	bytes     atomic.Int64
	resyncs   atomic.Int64
	active    atomic.Int64
	startedAt time.Time
}

// Server accepts NDTP connections and turns realtime frames into
// TelemetryPoints.
//
// The emulator is the TCP client and opens one connection per unitId, so the
// server is a plain listener with a read loop per connection. A broken
// connection is never fatal: the emulator reconnects and re-handshakes, and
// the receiver simply starts a new session.
type Server struct {
	opts     Options
	logger   *slog.Logger
	counters counters

	mu       sync.Mutex
	listener net.Listener
	conns    map[net.Conn]struct{}
	closed   bool
	wg       sync.WaitGroup
}

// NewServer creates a receiver. Call Listen, then Serve.
func NewServer(opts Options) *Server {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.IdleTimeout == 0 {
		opts.IdleTimeout = defaultIdleTimeout
	}
	if opts.WriteTimeout == 0 {
		opts.WriteTimeout = defaultWriteTimeout
	}
	return &Server{
		opts:   opts,
		logger: opts.Logger,
		conns:  make(map[net.Conn]struct{}),
	}
}

// Listen binds the TCP address. It is separate from Serve so tests can learn
// the port chosen by the kernel.
func (s *Server) Listen() error {
	listener, err := net.Listen("tcp", s.opts.Addr)
	if err != nil {
		return fmt.Errorf("ndtp: listen on %s: %w", s.opts.Addr, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		listener.Close()
		return net.ErrClosed
	}
	s.listener = listener
	s.counters.startedAt = time.Now()
	return nil
}

// Addr reports the bound address, or nil before Listen succeeds.
func (s *Server) Addr() net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener == nil {
		return nil
	}
	return s.listener.Addr()
}

// Serve accepts connections until ctx is cancelled or the server is closed.
func (s *Server) Serve(ctx context.Context) error {
	s.mu.Lock()
	listener := s.listener
	s.mu.Unlock()
	if listener == nil {
		return errors.New("ndtp: Serve called before Listen")
	}

	go func() {
		<-ctx.Done()
		s.Close()
	}()

	s.logger.Info("ndtp receiver listening", "addr", listener.Addr().String())
	for {
		conn, err := listener.Accept()
		if err != nil {
			if s.isClosed() || ctx.Err() != nil {
				return nil
			}
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				continue
			}
			return fmt.Errorf("ndtp: accept: %w", err)
		}
		s.counters.accepted.Add(1)
		s.counters.active.Add(1)
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handle(ctx, conn)
		}()
	}
}

// Close stops the listener and tears down every live connection. It is safe to
// call more than once.
func (s *Server) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	listener := s.listener
	conns := make([]net.Conn, 0, len(s.conns))
	for conn := range s.conns {
		conns = append(conns, conn)
	}
	s.mu.Unlock()

	var err error
	if listener != nil {
		err = listener.Close()
	}
	// Unblock the read loops.
	for _, conn := range conns {
		conn.Close()
	}
	s.wg.Wait()
	return err
}

func (s *Server) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// Stats returns a snapshot of the receiver counters.
func (s *Server) Stats() Stats {
	c := &s.counters
	return Stats{
		AcceptedConnections: c.accepted.Load(),
		ClosedConnections:   c.closed.Load(),
		ActiveConnections:   c.active.Load(),
		Handshakes:          c.handshake.Load(),
		RealtimeFrames:      c.realtime.Load(),
		FramesTotal:         c.frames.Load(),
		CRCFailed:           c.crcFailed.Load(),
		CellDecodeErrors:    c.cellError.Load(),
		TelemetryPoints:     c.telemetry.Load(),
		NavMissing:          c.navMissed.Load(),
		PeerMismatch:        c.mismatch.Load(),
		BytesRead:           c.bytes.Load(),
		Resyncs:             c.resyncs.Load(),
		StartedAt:           c.startedAt,
	}
}

func (s *Server) trackConn(conn net.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.conns[conn] = struct{}{}
	return true
}

func (s *Server) untrackConn(conn net.Conn) {
	s.mu.Lock()
	delete(s.conns, conn)
	s.mu.Unlock()
}

// handle runs the read loop for one connection until it fails or is closed.
func (s *Server) handle(ctx context.Context, conn net.Conn) {
	defer func() {
		s.untrackConn(conn)
		conn.Close()
		s.counters.active.Add(-1)
		s.counters.closed.Add(1)
	}()

	if !s.trackConn(conn) {
		return
	}
	remote := conn.RemoteAddr().String()
	s.logger.Info("ndtp connection opened", "remote", remote)

	reader := NewHeaderReader(conn)
	var vehicle uint32
	var haveVehicle bool
	// Byte and resync counters live on the reader, so track how much of them
	// has already been added to the shared totals.
	var countedBytes, countedResyncs int64

	for {
		if ctx.Err() != nil {
			return
		}
		if s.opts.IdleTimeout > 0 {
			// Best effort: a dead peer must not hold a goroutine forever.
			_ = conn.SetReadDeadline(time.Now().Add(s.opts.IdleTimeout))
		}

		frame, err := reader.NextFrame()
		if err != nil {
			s.flushReaderCounters(reader, &countedBytes, &countedResyncs)
			if ctx.Err() == nil && !isExpectedDisconnect(err) {
				s.logger.Warn("ndtp frame error",
					"remote", remote, "vehicle", vehicle, "error", err)
			}
			return
		}
		s.counters.frames.Add(1)
		s.flushReaderCounters(reader, &countedBytes, &countedResyncs)

		if !frame.CRCValid {
			s.counters.crcFailed.Add(1)
			s.logger.Warn("ndtp frame failed CRC, using it anyway",
				"remote", remote, "peer", frame.Peer, "service", frame.ServiceID)
		}

		switch {
		case frame.IsHandshake():
			s.counters.handshake.Add(1)
			id := frame.Peer
			if peer, ok := handshakePeer(frame.Body); ok && peer != 0 && peer != id {
				s.counters.mismatch.Add(1)
				s.logger.Warn("ndtp handshake peer differs from NPL peer",
					"remote", remote, "npl_peer", id, "body_peer", peer)
			}
			vehicle, haveVehicle = id, true
			s.logger.Info("ndtp handshake", "remote", remote, "vehicle", id)
			s.replyHandshake(conn, frame)

		case frame.IsRealtime():
			s.counters.realtime.Add(1)
			if !haveVehicle {
				// The specification has the emulator handshake first, but a
				// realtime frame still carries the peer, so keep going.
				vehicle, haveVehicle = frame.Peer, true
			}
			s.handleRealtime(remote, vehicle, frame)

		default:
			s.logger.Debug("ndtp ignoring frame",
				"remote", remote, "service", frame.ServiceID, "type", frame.MessageType)
		}
	}
}

// handleRealtime extracts the navigation cell and hands it to the consumer.
func (s *Server) handleRealtime(remote string, vehicle uint32, frame Frame) {
	cells, err := DecodeCells(frame.Body)
	if err != nil {
		// The navigation cell is always first, so cells may still be usable.
		s.counters.cellError.Add(1)
		s.logger.Warn("ndtp cell decode stopped early",
			"remote", remote, "vehicle", vehicle, "cells", len(cells), "error", err)
	}
	if len(cells) == 0 || cells[0].Type != CellNav00 {
		s.counters.navMissed.Add(1)
		s.logger.Warn("ndtp realtime frame without navigation cell",
			"remote", remote, "vehicle", vehicle, "cells", len(cells))
		return
	}

	nav, err := DecodeNavCell(cells[0].Data)
	if err != nil {
		s.counters.cellError.Add(1)
		s.logger.Warn("ndtp navigation decode failed", "remote", remote, "vehicle", vehicle, "error", err)
		return
	}
	if vehicle == 0 {
		vehicle = frame.Peer
	}
	s.counters.telemetry.Add(1)
	if s.opts.OnTelemetry == nil {
		return
	}
	s.opts.OnTelemetry(TelemetryPoint{
		VehicleID:     vehicle,
		Nav:           nav,
		ReceivedAt:    time.Now(),
		CellsTrailing: len(cells) - 1,
	})
}

// flushReaderCounters folds a connection's cumulative byte and resync counts
// into the server totals, so concurrent connections add up instead of racing to
// overwrite each other.
func (s *Server) flushReaderCounters(reader *HeaderReader, countedBytes, countedResyncs *int64) {
	if n := reader.BytesRead(); n > *countedBytes {
		s.counters.bytes.Add(n - *countedBytes)
		*countedBytes = n
	}
	if n := reader.Resyncs(); n > *countedResyncs {
		s.counters.resyncs.Add(n - *countedResyncs)
		*countedResyncs = n
	}
}

// replyHandshake sends one best-effort acknowledgement. The emulator does not
// parse replies, but the specification expects the receiver to answer, and
// writing the bytes confirms the socket is live. Exactly one reply is sent per
// connection: the emulator never reads again, so writing more would only risk
// filling the socket buffer.
func (s *Server) replyHandshake(conn net.Conn, frame Frame) {
	_ = conn.SetWriteDeadline(time.Now().Add(s.opts.WriteTimeout))
	defer conn.SetWriteDeadline(time.Time{})

	body := BuildHandshakeBody(frame.Peer)
	ack := buildFrame(frame.Peer, ServiceGenericControls, MsgConnRequest, frame.RequestID, 0, body)
	if _, err := conn.Write(ack); err != nil {
		s.logger.Debug("ndtp handshake reply not delivered", "peer", frame.Peer, "error", err)
	}
}

// handshakePeer reads peerAddress out of an 18-byte handshake body.
func handshakePeer(body []byte) (uint32, bool) {
	if len(body) < handshakeBodySize {
		return 0, false
	}
	return binary.LittleEndian.Uint32(body[6:10]), true
}

func isExpectedDisconnect(err error) bool {
	if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr)
}
