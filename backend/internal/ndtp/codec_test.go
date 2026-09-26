package ndtp

import (
	"bytes"
	"encoding/binary"

	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// slowReader hands out at most n bytes per Read, emulating a TCP stream that
// delivers a frame across several segments.
type slowReader struct {
	data []byte
	n    int
}

func (s *slowReader) Read(p []byte) (int, error) {
	if len(s.data) == 0 {
		return 0, io.EOF
	}
	n := min(min(s.n, len(p)), len(s.data))
	copy(p, s.data[:n])
	s.data = s.data[n:]
	return n, nil
}

func TestBuildAndReadHandshakeFrame(t *testing.T) {
	peer := uint32(1166336)
	frame := BuildFrame(peer, ServiceGenericControls, MsgConnRequest, 1, BuildHandshakeBody(peer))

	r := NewHeaderReader(bytes.NewReader(frame))
	got, err := r.NextFrame()
	require.NoError(t, err)

	assert.Equal(t, peer, got.Peer)
	assert.True(t, got.IsHandshake())
	assert.False(t, got.IsRealtime())
	assert.Equal(t, uint32(1), got.RequestID)
	assert.True(t, got.CRCValid)
	require.Len(t, got.Body, handshakeBodySize)
	assert.Equal(t, protoVersionHigh, uint16(got.Body[0])|uint16(got.Body[1])<<8)
	assert.Equal(t, protoVersionLow, uint16(got.Body[2])|uint16(got.Body[3])<<8)
}

func TestBuildAndReadRealtimeFrame(t *testing.T) {
	peer := uint32(893159) // tr_id 122048 in the dataset
	nav := NavCell{
		Timestamp: time.Unix(1767665400, 0).UTC(),
		Longitude: 37.617321, Latitude: 55.7551234,
		SpeedAvg: 24, Course: 180,
		Flags: NavFlags{North: true, East: true, Valid: true},
	}
	body := AppendCell(nil, CellNav00, 0, EncodeNavCell(nav))
	body = AppendCell(body, CellUsi08, 0, make([]byte, 6))

	frame := BuildFrame(peer, ServiceNavData, MsgRealtime, 42, body)

	got, err := NewHeaderReader(bytes.NewReader(frame)).NextFrame()
	require.NoError(t, err)
	assert.True(t, got.IsRealtime())
	assert.Equal(t, peer, got.Peer)
	assert.Equal(t, uint32(42), got.RequestID)
	assert.True(t, got.CRCValid)

	cells, err := DecodeCells(got.Body)
	require.NoError(t, err)
	require.Len(t, cells, 2)
	decoded, err := DecodeNavCell(cells[0].Data)
	require.NoError(t, err)
	assert.InDelta(t, 37.617321, decoded.Longitude, 1e-7)
	assert.InDelta(t, 55.7551234, decoded.Latitude, 1e-7)
	assert.Equal(t, 24.0, decoded.SpeedAvg)
}

// corruptBodyByte flips the first byte of a frame body, leaving the NPL header
// (and therefore the declared CRC) intact.
func corruptBodyByte(t *testing.T, frame []byte) []byte {
	t.Helper()
	require.Greater(t, len(frame), nplSize+nphSize)
	out := append([]byte(nil), frame...)
	out[nplSize+nphSize] ^= 0xFF
	return out
}

func TestHeaderReaderValidatesCRC(t *testing.T) {
	frame := corruptBodyByte(t, BuildFrame(42, ServiceNavData, MsgRealtime, 1, []byte{1, 2, 3, 4, 5}))

	got, err := NewHeaderReader(bytes.NewReader(frame)).NextFrame()
	require.NoError(t, err)
	assert.False(t, got.CRCValid, "a corrupted body must fail CRC validation")
}

func TestHeaderReaderDetectsSwappedCRCMismatch(t *testing.T) {
	frame := BuildFrame(42, ServiceNavData, MsgRealtime, 1, []byte{1, 2, 3})
	// The NPL stores the CRC byte-swapped; writing it unswapped must not validate.
	stored := binary.LittleEndian.Uint16(frame[6:8])
	binary.LittleEndian.PutUint16(frame[6:8], SwapBytes16(stored))

	got, err := NewHeaderReader(bytes.NewReader(frame)).NextFrame()
	require.NoError(t, err)
	assert.False(t, got.CRCValid)
}

func TestHeaderReaderResyncsPastGarbage(t *testing.T) {
	frame := BuildFrame(7, ServiceNavData, MsgRealtime, 1, []byte{9, 9, 9})

	// Garbage that contains no 0x7E 0x7E signature pair.
	garbage := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x00, 0xFF, 0x10, 0x20}

	r := NewHeaderReader(bytes.NewReader(append(garbage, frame...)))
	got, err := r.NextFrame()
	require.NoError(t, err)
	assert.Equal(t, uint32(7), got.Peer)
	assert.True(t, got.CRCValid)
}

// readRecovering mimics the server read loop: log the framing error and try
// again. A stray 0x7E 0x7E pair in garbage is indistinguishable from a real
// signature, so it costs one logged error and then the reader locks on.
func readRecovering(t *testing.T, r *HeaderReader) Frame {
	t.Helper()
	var lastErr error
	for range 10 {
		frame, err := r.NextFrame()
		if err == nil {
			return frame
		}
		lastErr = err
	}
	t.Fatalf("no frame after 10 attempts, last error: %v", lastErr)
	return Frame{}
}

func TestHeaderReaderResyncsPastRepeatedSignatureBytes(t *testing.T) {
	frame := BuildFrame(11, ServiceNavData, MsgRealtime, 3, []byte{1, 2})

	// A lone 0x7E, then a false 0x7E 0x7E pair followed by bytes that cannot
	// form a header, then another lone 0x7E. The real frame follows.
	//
	// Note the last prefix byte is deliberately not 0x7E: a lone 0x7E sitting
	// immediately before a real signature creates an overlapping candidate at
	// the byte before it. That candidate is indistinguishable from a real
	// header until its type, size and CRC are checked, so it is resolved by
	// validation rather than by scanning.
	prefix := []byte{0x7E, 0x00, 0x7E, 0x7E, 0x01, 0x7E, 0x02}
	r := NewHeaderReader(bytes.NewReader(append(prefix, frame...)))

	got := readRecovering(t, r)
	assert.Equal(t, uint32(11), got.Peer)
	assert.Equal(t, uint32(3), got.RequestID)
	assert.True(t, got.CRCValid)
	// The false pair at offset 2 was abandoned.
	assert.EqualValues(t, 1, r.Resyncs())
}

func TestHeaderReaderCountsRepeatedFalseSignatures(t *testing.T) {
	frame := BuildFrame(12, ServiceNavData, MsgRealtime, 1, []byte{7})
	prefix := []byte{0x7E, 0x7E, 0x01, 0x02, 0x7E, 0x7E, 0x03, 0x04, 0x7E, 0x7E, 0x05, 0x06}

	r := NewHeaderReader(bytes.NewReader(append(prefix, frame...)))
	got := readRecovering(t, r)
	assert.Equal(t, uint32(12), got.Peer)
	assert.EqualValues(t, 3, r.Resyncs())
}

func TestHeaderReaderReadsConsecutiveFrames(t *testing.T) {
	var stream []byte
	for i := range 5 {
		stream = append(stream, BuildFrame(uint32(100+i), ServiceNavData, MsgRealtime, uint32(i), []byte{byte(i)})...)
	}

	r := NewHeaderReader(bytes.NewReader(stream))
	for i := range 5 {
		got, err := r.NextFrame()
		require.NoError(t, err, "frame %d", i)
		assert.Equal(t, uint32(100+i), got.Peer)
		assert.Equal(t, uint32(i), got.RequestID)
		assert.True(t, got.CRCValid)
		assert.Equal(t, []byte{byte(i)}, got.Body)
	}
}

func TestHeaderReaderHandlesFrameSplitAcrossReads(t *testing.T) {
	var stream []byte
	for i := range 3 {
		nav := EncodeNavCell(NavCell{
			Timestamp: time.Unix(int64(1767665400+i), 0).UTC(),
			Longitude: 37.6, Latitude: 55.7,
			Flags: NavFlags{North: true, East: true, Valid: true},
		})
		stream = append(stream, BuildFrame(uint32(500+i), ServiceNavData, MsgRealtime, uint32(i), AppendCell(nil, CellNav00, 0, nav))...)
	}

	// One byte at a time is the worst case a TCP stream can produce.
	r := NewHeaderReader(&slowReader{data: stream, n: 1})
	for i := range 3 {
		got, err := r.NextFrame()
		require.NoError(t, err, "frame %d", i)
		assert.Equal(t, uint32(500+i), got.Peer)
		require.True(t, got.CRCValid)

		cells, err := DecodeCells(got.Body)
		require.NoError(t, err)
		nav, err := DecodeNavCell(cells[0].Data)
		require.NoError(t, err)
		assert.Equal(t, time.Unix(int64(1767665400+i), 0).UTC(), nav.Timestamp)
	}
}

func TestHeaderReaderReportsUnexpectedEOF(t *testing.T) {
	full := BuildFrame(1, ServiceNavData, MsgRealtime, 1, make([]byte, 40))

	// Cut the frame in the middle of its body.
	r := NewHeaderReader(bytes.NewReader(full[:len(full)-10]))
	_, err := r.NextFrame()
	require.Error(t, err)
	assert.ErrorIs(t, err, io.ErrUnexpectedEOF)

	var typed *FrameError
	require.ErrorAs(t, err, &typed)
}

func TestHeaderReaderReportsCleanEOF(t *testing.T) {
	_, err := NewHeaderReader(bytes.NewReader(nil)).NextFrame()
	require.Error(t, err)
	assert.ErrorIs(t, err, io.EOF)
}

func TestHeaderReaderRejectsOutOfRangeDataSize(t *testing.T) {
	// dataSize below the NPH header size is impossible, so the candidate is
	// abandoned rather than believed.
	frame := BuildFrame(1, ServiceNavData, MsgRealtime, 1, nil)
	frame[2] = 3
	frame[3] = 0

	r := NewHeaderReader(bytes.NewReader(frame))
	_, err := r.NextFrame()
	require.Error(t, err)
	assert.ErrorIs(t, err, io.EOF, "the bogus candidate should be skipped and the stream end")
	assert.EqualValues(t, 1, r.Resyncs())
}

func TestHeaderReaderRejectsUnsupportedNPLType(t *testing.T) {
	frame := BuildFrame(1, ServiceNavData, MsgRealtime, 1, nil)
	frame[8] = 0x07

	r := NewHeaderReader(bytes.NewReader(frame))
	_, err := r.NextFrame()
	require.Error(t, err)
	assert.ErrorIs(t, err, io.EOF)
	assert.EqualValues(t, 1, r.Resyncs())
}

func TestHeaderReaderRecoversAfterCorruptFrame(t *testing.T) {
	good := BuildFrame(99, ServiceNavData, MsgRealtime, 5, []byte{0xAA, 0xBB})
	corrupt := corruptBodyByte(t, BuildFrame(98, ServiceNavData, MsgRealtime, 4, []byte{0xCC, 0xDD}))

	stream := append(append([]byte(nil), corrupt...), good...)
	r := NewHeaderReader(bytes.NewReader(stream))

	first, err := r.NextFrame()
	require.NoError(t, err)
	assert.False(t, first.CRCValid)

	second, err := r.NextFrame()
	require.NoError(t, err)
	assert.Equal(t, uint32(99), second.Peer)
	assert.True(t, second.CRCValid)
}

func TestHeaderReaderBytesReadCountsDiscardedGarbage(t *testing.T) {
	frame := BuildFrame(1, ServiceNavData, MsgRealtime, 1, []byte{1})
	garbage := []byte{0x01, 0x02, 0x03}

	r := NewHeaderReader(bytes.NewReader(append(garbage, frame...)))
	_, err := r.NextFrame()
	require.NoError(t, err)
	assert.Equal(t, int64(len(garbage)+len(frame)), r.BytesRead())
}

func TestFrameKindPredicates(t *testing.T) {
	handshake := Frame{ServiceID: ServiceGenericControls, MessageType: MsgConnRequest}
	assert.True(t, handshake.IsHandshake())
	assert.False(t, handshake.IsRealtime())

	realtime := Frame{ServiceID: ServiceNavData, MessageType: MsgRealtime}
	assert.True(t, realtime.IsRealtime())
	assert.False(t, realtime.IsHandshake())

	other := Frame{ServiceID: 7, MessageType: 7}
	assert.False(t, other.IsHandshake())
	assert.False(t, other.IsRealtime())
}

func TestFrameErrorIncludesOffset(t *testing.T) {
	// Two garbage bytes: the first is discarded, then the stream ends.
	_, err := NewHeaderReader(bytes.NewReader([]byte{0x01, 0x02})).NextFrame()
	require.Error(t, err)
	var typed *FrameError
	require.ErrorAs(t, err, &typed)
	assert.EqualValues(t, 1, typed.Offset)
	assert.ErrorIs(t, err, io.EOF)
}
