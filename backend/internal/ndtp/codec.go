package ndtp

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
)

// Fixed frame geometry, all little-endian and packed (no alignment padding).
const (
	nplSize = 15 // [signature:2][dataSize:2][flags:2][crc:2][type:1][peer:4][requestId:2]
	nphSize = 10 // [serviceId:2][type:2][flags:2][requestId:4]

	// handshakeBodySize is the length of an NPH_SGC_CONN_REQUEST body.
	handshakeBodySize = 18

	// Signature is the NPL magic that starts every frame.
	Signature uint16 = 0x7E7E
	// signatureByte is the first byte of Signature, used when scanning.
	signatureByte = 0x7E
	// TypeNPH is the NPL frame type for an NPH frame.
	TypeNPH byte = 0x02

	// ServiceGenericControls / ServiceNavData are NPH service identifiers.
	ServiceGenericControls uint16 = 0
	ServiceNavData         uint16 = 1

	// MsgConnRequest / MsgRealtime are NPH message types.
	MsgConnRequest uint16 = 100
	MsgRealtime    uint16 = 101

	protoVersionHigh uint16 = 6
	protoVersionLow  uint16 = 2

	// maxDataSize bounds dataSize so a corrupt length cannot make the reader
	// allocate without limit. 65535 is the maxPacketSize the emulator declares.
	maxDataSize = 65535
)

// Frame is a decoded NPL header plus the NPH header that follows it. Body is
// not copied and stays valid only until the next read on the same Reader.
type Frame struct {
	Peer        uint32
	ServiceID   uint16
	MessageType uint16
	RequestID   uint32
	CRCValid    bool
	Body        []byte
}

// IsHandshake reports whether the frame is an NPH_SGC_CONN_REQUEST.
func (f Frame) IsHandshake() bool {
	return f.ServiceID == ServiceGenericControls && f.MessageType == MsgConnRequest
}

// IsRealtime reports whether the frame carries telemetry.
func (f Frame) IsRealtime() bool {
	return f.ServiceID == ServiceNavData && f.MessageType == MsgRealtime
}

// FrameError describes a frame that could not be parsed.
//
// A frame whose CRC does not match is not an error: it is returned with
// CRCValid false, because the navigation data may still be usable. FrameError
// is reserved for a stream that ended or a body that could not be read in full.
// The reader stays synchronised after one, so callers should log it and read
// again.
type FrameError struct {
	Reason string
	// Err is the underlying cause, if any (io.EOF, io.ErrUnexpectedEOF, ...).
	Err error
	// Offset is the number of bytes consumed from the connection, including
	// bytes discarded while resynchronising.
	Offset int64
}

func (e *FrameError) Error() string {
	return fmt.Sprintf("ndtp: %s (at byte %d)", e.Reason, e.Offset)
}

func (e *FrameError) Unwrap() error { return e.Err }

// HeaderReader reads NDTP frames from a stream. It is not safe for concurrent
// use; give each connection its own.
type HeaderReader struct {
	br       *bufio.Reader
	consumed int64
	resyncs  int64
}

// NewHeaderReader wraps r. Read deadlines are the caller's responsibility; the
// NDTP connection is long-lived, so prefer setting a deadline only while a
// frame is expected.
func NewHeaderReader(r io.Reader) *HeaderReader {
	return &HeaderReader{br: bufio.NewReaderSize(r, 4096)}
}

// NextFrame reads the next frame.
//
// The reader skips leading garbage and recovers from a false 0x7E7E candidate
// on its own, so a desynchronised stream heals without the caller doing
// anything. Two conditions are still returned as *FrameError and are always
// recoverable by calling again: the stream ended, or the declared body could
// not be read in full.
//
// A frame whose CRC does not match is not an error. It is returned with
// CRCValid false so the caller can count it and decide.
func (h *HeaderReader) NextFrame() (Frame, error) {
	for {
		header, err := h.peekHeader()
		if err != nil {
			return Frame{}, h.frameError(err)
		}
		if header[8] != TypeNPH {
			h.skipCandidate()
			continue
		}
		dataSize := int(binary.LittleEndian.Uint16(header[2:4]))
		if dataSize < nphSize || dataSize > maxDataSize {
			h.skipCandidate()
			continue
		}
		if err := h.discard(nplSize); err != nil {
			return Frame{}, h.frameError(err)
		}
		h.consumed += nplSize

		rest := make([]byte, dataSize)
		if _, err := io.ReadFull(h.br, rest); err != nil {
			return Frame{}, h.frameError(err)
		}
		h.consumed += int64(dataSize)

		frame := Frame{
			Peer:        binary.LittleEndian.Uint32(header[9:13]),
			ServiceID:   binary.LittleEndian.Uint16(rest[0:2]),
			MessageType: binary.LittleEndian.Uint16(rest[2:4]),
			RequestID:   binary.LittleEndian.Uint32(rest[6:10]),
			Body:        rest[nphSize:],
		}
		want := SwapBytes16(binary.LittleEndian.Uint16(header[6:8]))
		frame.CRCValid = CRC16Modbus(rest) == want
		return frame, nil
	}
}

// peekHeader advances to the next 0x7E7E signature and returns the NPL header
// bytes without consuming them, so a candidate that turns out not to be a real
// frame header costs only its two signature bytes.
func (h *HeaderReader) peekHeader() ([]byte, error) {
	for {
		window, err := h.br.Peek(2)
		if err != nil {
			return nil, err
		}
		if window[0] == signatureByte && window[1] == signatureByte {
			// bufio.Peek does not advance, so the bytes stay available for the
			// Discard that follows once the header has been validated.
			return h.br.Peek(nplSize)
		}
		if _, err := h.br.Discard(1); err != nil {
			return nil, err
		}
		h.consumed++
	}
}

// skipCandidate abandons a 0x7E7E that did not validate as a frame header and
// resumes scanning after it.
func (h *HeaderReader) skipCandidate() {
	if err := h.discard(2); err == nil {
		h.consumed += 2
		h.resyncs++
	}
}

func (h *HeaderReader) discard(n int) error {
	discarded, err := h.br.Discard(n)
	if err != nil {
		return err
	}
	if discarded != n {
		return io.ErrUnexpectedEOF
	}
	return nil
}

// Resyncs reports how many false 0x7E7E candidates have been abandoned. A
// non-zero, growing count means the stream is carrying noise.
func (h *HeaderReader) Resyncs() int64 { return h.resyncs }

func (h *HeaderReader) frameError(err error) error {
	return &FrameError{Reason: err.Error(), Err: err, Offset: h.consumed}
}

// BytesRead reports how many bytes have been consumed from the connection,
// including bytes discarded while resynchronising.
func (h *HeaderReader) BytesRead() int64 { return h.consumed }
