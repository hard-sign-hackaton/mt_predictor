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

// TelemetryPoint — одна декодированная навигационная запись из live-фрейма.
// Приёмник передаёт её остальному backend без выдуманных маршрутных данных.
type TelemetryPoint struct {
	// VehicleID — NPL.peerAddress, то есть unit_id бортового терминала.
	// Это не tr_id: связь с расписанием отдельно берётся из каталога привязок.
	VehicleID  uint32
	Nav        NavCell
	ReceivedAt time.Time
	// CellsTrailing — сколько ячеек шло после навигационной. Поле нужно только для
	// наблюдаемости: модель задержки использует навигационную ячейку.
	CellsTrailing int
}

// Options задаёт параметры приёмника.
type Options struct {
	// Addr — TCP-адрес прослушивания, например ":9201". К нему подключается эмулятор.
	Addr string
	// OnTelemetry вызывается для каждого realtime-кадра, из которого удалось
	// получить навигационную ячейку. Функция выполняется в горутине
	// соединения и не должна блокировать.
	OnTelemetry func(TelemetryPoint)
	// Logger получает события жизненного цикла соединений и предупреждения декодера.
	// По умолчанию используется slog.Default().
	Logger *slog.Logger
	// IdleTimeout закрывает соединение, из которого столько времени не было данных.
	// Нулевое значение отключает дедлайн.
	IdleTimeout time.Duration
	// WriteTimeout ограничивает ответ на рукопожатие по принципу «попробовать и забыть».
	// По умолчанию 5 секунд.
	WriteTimeout time.Duration
}

const (
	defaultIdleTimeout  = 5 * time.Minute
	defaultWriteTimeout = 5 * time.Second
)

// Stats — снимок счётчиков приёмника.
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

// Server принимает соединения NDTP и превращает realtime-кадры в
// TelemetryPoint.
// TelemetryPoints.
//
// Эмулятор выступает TCP-клиентом и открывает по соединению на каждый unitId,
// поэтому приёмник устроен как обычный слушатель с циклом чтения на
// соединение. Оборванное соединение никогда не считается фатальным:
// эмулятор переподключается и заново проходит рукопожатие, а приёмник
// просто начинает новую сессию.
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

// NewServer создаёт приёмник. Сначала вызывается Listen, затем Serve.
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

// Listen занимает TCP-адрес. Вынесено отдельно от Serve, чтобы тесты могли узнать
// порт, выбранный ядром.
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

// Addr сообщает занятый адрес либо nil, если Listen ещё не отработал успешно.
func (s *Server) Addr() net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener == nil {
		return nil
	}
	return s.listener.Addr()
}

// Serve принимает соединения, пока не отменён ctx или не закрыт приёмник.
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

// Close останавливает слушатель и закрывает все живые соединения. Повторный вызов
// безопасен.
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
	// Разблокировать циклы чтения.
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

// Stats возвращает снимок счётчиков приёмника.
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

// handle выполняет цикл чтения одного соединения до сбоя или закрытия.
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
	// Счётчики байт и повторных синхронизаций живут в читателе, поэтому
	// запоминаем, сколько из них уже учтено в общих итогах.
	var countedBytes, countedResyncs int64

	for {
		if ctx.Err() != nil {
			return
		}
		if s.opts.IdleTimeout > 0 {
			// Попытка сделать хорошо: мёртвый узел не должен удерживать горутину.
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
				// По спецификации эмулятор сначала здоровается, однако
				// realtime-кадр всё равно несёт peer, поэтому продолжаем работу.
				vehicle, haveVehicle = frame.Peer, true
			}
			s.handleRealtime(remote, vehicle, frame)

		default:
			s.logger.Debug("ndtp ignoring frame",
				"remote", remote, "service", frame.ServiceID, "type", frame.MessageType)
		}
	}
}

// handleRealtime извлекает навигационную ячейку и передаёт её получателю.
func (s *Server) handleRealtime(remote string, vehicle uint32, frame Frame) {
	cells, err := DecodeCells(frame.Body)
	if err != nil {
		// Навигационная ячейка всегда первая, поэтому разобранные ячейки
		// могут ещё пригодиться.
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

// flushReaderCounters переносит накопленные счётчики байт и повторных
// синхронизаций одного соединения в общие итоги, чтобы параллельные соединения
// складывались, а не затирали друг друга.
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

// replyHandshake отправляет один ответ на рукопожатие по принципу «попробовать и
// забыть». Эмулятор ответы не разбирает, однако по спецификации приёмник
// обязан ответить, а запись байт подтверждает, что сокет жив. Ровно один ответ
// отправляется на соединение: эмулятор больше не читает, поэтому лишняя запись
// лишь рискует заполнить буфер сокета.
func (s *Server) replyHandshake(conn net.Conn, frame Frame) {
	_ = conn.SetWriteDeadline(time.Now().Add(s.opts.WriteTimeout))
	defer conn.SetWriteDeadline(time.Time{})

	body := BuildHandshakeBody(frame.Peer)
	ack := buildFrame(frame.Peer, ServiceGenericControls, MsgConnRequest, frame.RequestID, 0, body)
	if _, err := conn.Write(ack); err != nil {
		s.logger.Debug("ndtp handshake reply not delivered", "peer", frame.Peer, "error", err)
	}
}

// handshakePeer читает peerAddress из 18-байтового тела рукопожатия.
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
