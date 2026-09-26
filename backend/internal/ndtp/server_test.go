package ndtp

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testServer starts a receiver on a random port and returns its address, the
// telemetry channel and the server itself.
func testServer(t *testing.T, mutate func(*Options)) (string, chan TelemetryPoint, *Server) {
	t.Helper()

	points := make(chan TelemetryPoint, 256)
	opts := Options{
		Addr:        "127.0.0.1:0",
		OnTelemetry: func(p TelemetryPoint) { points <- p },
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	if mutate != nil {
		mutate(&opts)
	}

	srv := NewServer(opts)
	require.NoError(t, srv.Listen())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		assert.NoError(t, srv.Serve(ctx))
	}()
	t.Cleanup(func() {
		cancel()
		srv.Close()
		<-done
	})

	return srv.Addr().String(), points, srv
}

// dial opens a client connection to the receiver.
func dial(t *testing.T, addr string) net.Conn {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })
	return conn
}

func writeAll(t *testing.T, conn net.Conn, data []byte) {
	t.Helper()
	_, err := conn.Write(data)
	require.NoError(t, err)
}

func awaitPoint(t *testing.T, points chan TelemetryPoint) TelemetryPoint {
	t.Helper()
	select {
	case p := <-points:
		return p
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for a telemetry point")
		return TelemetryPoint{}
	}
}

func navFrame(vehicle uint32, requestID uint32, nav NavCell) []byte {
	body := AppendCell(nil, CellNav00, 0, EncodeNavCell(nav))
	body = AppendCell(body, CellUsi08, 0, make([]byte, 6))
	return BuildFrame(vehicle, ServiceNavData, MsgRealtime, requestID, body)
}

func testNav(sec int64) NavCell {
	return NavCell{
		Timestamp: time.Unix(1767665400+sec, 0).UTC(),
		Longitude: 37.617321, Latitude: 55.7551234,
		SpeedAvg: 24, Course: 180, Altitude: 150,
		Flags: NavFlags{North: true, East: true, Valid: true},
	}
}

func TestServerReceivesHandshakeAndTelemetry(t *testing.T) {
	addr, points, srv := testServer(t, nil)
	conn := dial(t, addr)

	const vehicle = uint32(893159)
	writeAll(t, conn, BuildFrame(vehicle, ServiceGenericControls, MsgConnRequest, 1, BuildHandshakeBody(vehicle)))
	writeAll(t, conn, navFrame(vehicle, 2, testNav(0)))

	point := awaitPoint(t, points)
	assert.Equal(t, vehicle, point.VehicleID)
	assert.InDelta(t, 37.617321, point.Nav.Longitude, 1e-7)
	assert.InDelta(t, 55.7551234, point.Nav.Latitude, 1e-7)
	assert.Equal(t, 24.0, point.Nav.SpeedAvg)
	assert.True(t, point.Nav.Flags.Valid)
	assert.Equal(t, 1, point.CellsTrailing, "the fuel sensor cell follows navigation")
	assert.False(t, point.ReceivedAt.IsZero())

	stats := srv.Stats()
	assert.EqualValues(t, 1, stats.Handshakes)
	assert.EqualValues(t, 1, stats.RealtimeFrames)
	assert.EqualValues(t, 1, stats.TelemetryPoints)
	assert.EqualValues(t, 0, stats.CRCFailed)
}

func TestServerAcceptsRealtimeWithoutHandshake(t *testing.T) {
	// The specification has the emulator handshake first, but the peer address
	// travels on every frame, so a stream that skips the handshake still works.
	addr, points, _ := testServer(t, nil)
	conn := dial(t, addr)

	const vehicle = uint32(786201)
	writeAll(t, conn, navFrame(vehicle, 1, testNav(0)))

	point := awaitPoint(t, points)
	assert.Equal(t, vehicle, point.VehicleID)
}

func TestServerHandlesSequenceOfFramesOnOneConnection(t *testing.T) {
	addr, points, srv := testServer(t, nil)
	conn := dial(t, addr)

	const vehicle = uint32(985940)
	var stream []byte
	stream = append(stream, BuildFrame(vehicle, ServiceGenericControls, MsgConnRequest, 1, BuildHandshakeBody(vehicle))...)
	const count = 25
	for i := range count {
		stream = append(stream, navFrame(vehicle, uint32(i+2), testNav(int64(i)))...)
	}
	writeAll(t, conn, stream)

	for i := range count {
		point := awaitPoint(t, points)
		assert.Equal(t, vehicle, point.VehicleID)
		assert.Equal(t, time.Unix(1767665400+int64(i), 0).UTC(), point.Nav.Timestamp, "frame %d", i)
	}
	assert.EqualValues(t, count, srv.Stats().RealtimeFrames)
}

func TestServerHandlesConcurrentVehicles(t *testing.T) {
	addr, points, srv := testServer(t, nil)

	// The emulator opens one connection per unitId, so several must be served
	// at once. Run with -race to catch sharing mistakes.
	vehicles := []uint32{893159, 913870, 786201, 985940, 1105498}
	const perVehicle = 20

	var wg sync.WaitGroup
	connections := make(chan net.Conn, len(vehicles))
	for _, vehicle := range vehicles {
		wg.Add(1)
		go func(vehicle uint32) {
			defer wg.Done()
			conn, err := net.Dial("tcp", addr)
			if !assert.NoError(t, err) {
				return
			}
			frame := BuildFrame(vehicle, ServiceGenericControls, MsgConnRequest, 1, BuildHandshakeBody(vehicle))
			if _, err := conn.Write(frame); !assert.NoError(t, err) {
				return
			}
			for i := range perVehicle {
				if _, err := conn.Write(navFrame(vehicle, uint32(i+2), testNav(int64(i)))); err != nil {
					return
				}
			}
			// Соединение остаётся открытым, пока тест не получит все уже
			// записанные пакеты: так проверка не зависит от поведения TCP reset.
			connections <- conn
		}(vehicle)
	}
	wg.Wait()

	seen := make(map[uint32]int)
	for range len(vehicles) * perVehicle {
		point := awaitPoint(t, points)
		seen[point.VehicleID]++
	}
	close(connections)
	for conn := range connections {
		_ = conn.Close()
	}
	for _, vehicle := range vehicles {
		assert.Equal(t, perVehicle, seen[vehicle], "vehicle %d", vehicle)
	}
	assert.EqualValues(t, len(vehicles)*perVehicle, srv.Stats().TelemetryPoints)
}

func TestServerDeliversFrameWithBadCRC(t *testing.T) {
	addr, points, srv := testServer(t, nil)
	conn := dial(t, addr)

	const vehicle = uint32(122048)
	corrupt := navFrame(vehicle, 1, testNav(0))
	corrupt[nplSize+nphSize+8] ^= 0xFF // flip a navigation payload byte
	writeAll(t, conn, corrupt)

	point := awaitPoint(t, points)
	assert.Equal(t, vehicle, point.VehicleID)

	// Counted separately: the data is still usable, the operator just needs to
	// know the link is noisy.
	assert.EqualValues(t, 1, srv.Stats().CRCFailed)
}

func TestServerSkipsFrameWithoutNavigationCell(t *testing.T) {
	addr, points, srv := testServer(t, nil)
	conn := dial(t, addr)

	const vehicle = uint32(130072)
	// A body with no G6CellNav00 at all.
	writeAll(t, conn, BuildFrame(vehicle, ServiceNavData, MsgRealtime, 1,
		AppendCell(nil, CellUsi08, 0, make([]byte, 6))))

	// A following good frame proves the connection survives.
	writeAll(t, conn, navFrame(vehicle, 2, testNav(0)))
	point := awaitPoint(t, points)
	assert.Equal(t, vehicle, point.VehicleID)
	assert.EqualValues(t, 1, srv.Stats().NavMissing)
	assert.EqualValues(t, 1, srv.Stats().TelemetryPoints)
}

func TestServerRecoversFromLeadingGarbage(t *testing.T) {
	addr, points, _ := testServer(t, nil)
	conn := dial(t, addr)

	const vehicle = uint32(133300)
	garbage := []byte{0x01, 0x02, 0x03, 0x7E, 0x7E, 0x04, 0x05}
	writeAll(t, conn, append(garbage, navFrame(vehicle, 1, testNav(0))...))

	point := awaitPoint(t, points)
	assert.Equal(t, vehicle, point.VehicleID)
}

func TestServerRepliesToHandshake(t *testing.T) {
	addr, _, _ := testServer(t, nil)
	conn := dial(t, addr)

	const vehicle = uint32(134040)
	writeAll(t, conn, BuildFrame(vehicle, ServiceGenericControls, MsgConnRequest, 7, BuildHandshakeBody(vehicle)))

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
	reply := make([]byte, nplSize+nphSize+handshakeBodySize)
	_, err := io.ReadFull(conn, reply)
	require.NoError(t, err, "the receiver must answer a handshake")

	frame, err := NewHeaderReader(bytes.NewReader(reply)).NextFrame()
	require.NoError(t, err)
	assert.True(t, frame.CRCValid)
	assert.Equal(t, vehicle, frame.Peer)
	assert.Equal(t, uint32(7), frame.RequestID)
	assert.True(t, frame.IsHandshake())
	// A reply is a response, so the request bit must be clear.
	assert.Equal(t, uint16(0), binary.LittleEndian.Uint16(reply[nplSize+4:nplSize+6]))
}

func TestServerHandlesReconnect(t *testing.T) {
	addr, points, srv := testServer(t, nil)

	const first, second = uint32(134494), uint32(135081)
	conn1 := dial(t, addr)
	writeAll(t, conn1, navFrame(first, 1, testNav(0)))
	assert.Equal(t, first, awaitPoint(t, points).VehicleID)
	conn1.Close()

	// The emulator reconnects and re-handshakes after a drop.
	conn2 := dial(t, addr)
	writeAll(t, conn2, BuildFrame(second, ServiceGenericControls, MsgConnRequest, 1, BuildHandshakeBody(second)))
	writeAll(t, conn2, navFrame(second, 2, testNav(0)))
	assert.Equal(t, second, awaitPoint(t, points).VehicleID)

	assert.GreaterOrEqual(t, srv.Stats().AcceptedConnections, int64(2))
}

func TestServerClosesIdleConnection(t *testing.T) {
	addr, points, srv := testServer(t, func(o *Options) { o.IdleTimeout = 80 * time.Millisecond })
	conn := dial(t, addr)

	const vehicle = uint32(129964)
	writeAll(t, conn, navFrame(vehicle, 1, testNav(0)))
	awaitPoint(t, points)

	require.Eventually(t, func() bool {
		return srv.Stats().ActiveConnections == 0
	}, 3*time.Second, 20*time.Millisecond, "idle connection should be reaped")

	// The receiver itself keeps listening.
	conn2 := dial(t, addr)
	writeAll(t, conn2, navFrame(vehicle, 2, testNav(1)))
	awaitPoint(t, points)
	assert.GreaterOrEqual(t, srv.Stats().TelemetryPoints, int64(1))
}

func TestServerStopsOnContextCancel(t *testing.T) {
	addr, points, srv := testServer(t, nil)
	conn := dial(t, addr)

	const vehicle = uint32(130238)
	writeAll(t, conn, navFrame(vehicle, 1, testNav(0)))
	awaitPoint(t, points)

	srv.Close()
	_, err := conn.Write(navFrame(vehicle, 2, testNav(1)))
	if err == nil {
		// The write may succeed into a closed socket buffer; the read side must
		// not deliver anything more.
		select {
		case <-points:
			t.Fatal("received telemetry after shutdown")
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func TestServerTracksHandshakePeerMismatch(t *testing.T) {
	addr, _, srv := testServer(t, nil)
	conn := dial(t, addr)

	// NPL peer and handshake body disagree.
	body := BuildHandshakeBody(999)
	writeAll(t, conn, BuildFrame(111, ServiceGenericControls, MsgConnRequest, 1, body))

	require.Eventually(t, func() bool {
		return srv.Stats().PeerMismatch == 1
	}, 3*time.Second, 20*time.Millisecond)
}

func TestServerSurvivesGarbageOnlyConnection(t *testing.T) {
	addr, _, srv := testServer(t, nil)

	bad := dial(t, addr)
	writeAll(t, bad, []byte{0x01, 0x02, 0x03, 0x04})
	bad.Close()

	// A well-behaved terminal connecting afterwards must still be served.
	const vehicle = uint32(132430)
	good := dial(t, addr)
	writeAll(t, good, navFrame(vehicle, 1, testNav(0)))

	require.Eventually(t, func() bool {
		return srv.Stats().TelemetryPoints == 1
	}, 3*time.Second, 20*time.Millisecond)
}

func TestServerStatsStartedAtAndActiveCount(t *testing.T) {
	addr, points, srv := testServer(t, nil)
	conn := dial(t, addr)
	const vehicle = uint32(122613)
	writeAll(t, conn, navFrame(vehicle, 1, testNav(0)))
	awaitPoint(t, points)

	stats := srv.Stats()
	assert.False(t, stats.StartedAt.IsZero())
	assert.EqualValues(t, 1, stats.AcceptedConnections)
	assert.EqualValues(t, 1, stats.ActiveConnections)
	assert.EqualValues(t, 1, stats.FramesTotal)
	assert.Greater(t, stats.BytesRead, int64(0))

	conn.Close()
	require.Eventually(t, func() bool {
		return srv.Stats().ActiveConnections == 0
	}, 3*time.Second, 20*time.Millisecond)
	assert.EqualValues(t, 1, srv.Stats().ClosedConnections)
}

func TestNewServerAppliesDefaults(t *testing.T) {
	srv := NewServer(Options{Addr: "127.0.0.1:0"})
	assert.Equal(t, defaultIdleTimeout, srv.opts.IdleTimeout)
	assert.Equal(t, defaultWriteTimeout, srv.opts.WriteTimeout)
	assert.NotNil(t, srv.logger)
}

func TestServeBeforeListenFails(t *testing.T) {
	srv := NewServer(Options{Addr: "127.0.0.1:0"})
	assert.Error(t, srv.Serve(context.Background()))
}

func TestServerCloseIsIdempotent(t *testing.T) {
	_, _, srv := testServer(t, nil)
	require.NoError(t, srv.Close())
	require.NoError(t, srv.Close())
}
