package ndtp

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
)

// Геометрия кадра фиксирована, все поля little-endian, структуры без выравнивания.
const (
	nplSize = 15 // [сигнатура:2][dataSize:2][флаги:2][crc:2][тип:1][peer:4][requestId:2]
	nphSize = 10 // [serviceId:2][тип:2][флаги:2][requestId:4]

	// handshakeBodySize — длина тела NPH_SGC_CONN_REQUEST.
	handshakeBodySize = 18

	// Signature — сигнатура NPL, с которой начинается каждый кадр.
	Signature uint16 = 0x7E7E
	// signatureByte — первый байт Signature, используется при сканировании.
	signatureByte = 0x7E
	// TypeNPH — тип кадра NPL для кадра NPH.
	TypeNPH byte = 0x02

	// ServiceGenericControls и ServiceNavData — идентификаторы сервисов NPH.
	ServiceGenericControls uint16 = 0
	ServiceNavData         uint16 = 1

	// MsgConnRequest и MsgRealtime — типы сообщений NPH.
	MsgConnRequest uint16 = 100
	MsgRealtime    uint16 = 101

	protoVersionHigh uint16 = 6
	protoVersionLow  uint16 = 2

	// maxDataSize ограничивает dataSize, чтобы испорченная длина не заставила
	// читателя выделять память без предела. 65535 — объявленный эмулятором
	// максимальный размер пакета.
	maxDataSize = 65535
)

// Frame — декодированный заголовок NPL вместе со следующим за ним заголовком NPH.
// Поле Body не копируется и действительно только до следующего чтения на том же
// Reader.
type Frame struct {
	Peer        uint32
	ServiceID   uint16
	MessageType uint16
	RequestID   uint32
	CRCValid    bool
	Body        []byte
}

// IsHandshake сообщает, что кадр является NPH_SGC_CONN_REQUEST.
func (f Frame) IsHandshake() bool {
	return f.ServiceID == ServiceGenericControls && f.MessageType == MsgConnRequest
}

// IsRealtime сообщает, что кадр несёт телеметрию.
func (f Frame) IsRealtime() bool {
	return f.ServiceID == ServiceNavData && f.MessageType == MsgRealtime
}

// FrameError описывает кадр, который не удалось разобрать.
type FrameError struct {
	Reason string
	// Err — исходная причина, если она есть: io.EOF, io.ErrUnexpectedEOF и тому подобное.
	Err error
	// Offset — число байт, прочитанных из соединения, включая байты, отброшенные
	// при повторной синхронизации.
	Offset int64
}

func (e *FrameError) Error() string {
	return fmt.Sprintf("ndtp: %s (at byte %d)", e.Reason, e.Offset)
}

func (e *FrameError) Unwrap() error { return e.Err }

// HeaderReader читает кадры NDTP из потока. Небезопасен при конкурентном
// использовании, поэтому каждому соединению нужен свой экземпляр.
type HeaderReader struct {
	br       *bufio.Reader
	consumed int64
	resyncs  int64
}

// NewHeaderReader оборачивает r. Дедлайны чтения остаются на стороне вызывающего
// кода: соединение NDTP долгоживущее, поэтому дедлайн лучше ставить
// только на время ожидания кадра.
func NewHeaderReader(r io.Reader) *HeaderReader {
	return &HeaderReader{br: bufio.NewReaderSize(r, 4096)}
}

// NextFrame читает следующий кадр.
//
// Читатель сам пропускает мусор в начале и восстанавливается после ложной
// последовательности 0x7E7E, поэтому рассинхронизированный поток
// восстанавливается без участия вызывающего кода. Два случая всё же
// возвращаются как *FrameError и всегда разрешаются повторным вызовом: поток
// оборвался либо заявленное тело не удалось прочитать целиком.
//
// Кадр с несовпавшей CRC ошибкой не считается: он возвращается с CRCValid
// false, чтобы вызывающий код мог его посчитать и решить сам.
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

// peekHeader продвигается до следующей сигнатуры 0x7E7E и возвращает байты
// заголовка NPL, не потребляя их, поэтому кандидат, оказавшийся не
// заголовком кадра, стоит всего двух байт сигнатуры.
func (h *HeaderReader) peekHeader() ([]byte, error) {
	for {
		window, err := h.br.Peek(2)
		if err != nil {
			return nil, err
		}
		if window[0] == signatureByte && window[1] == signatureByte {
			// bufio.Peek не продвигает позицию, поэтому байты остаются
			// доступными для Discard после проверки заголовка.
			return h.br.Peek(nplSize)
		}
		if _, err := h.br.Discard(1); err != nil {
			return nil, err
		}
		h.consumed++
	}
}

// skipCandidate отбрасывает 0x7E7E, не прошедшую проверку как заголовок
// кадра, и продолжает сканирование после неё.
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

// Resyncs сообщает, сколько ложных последовательностей 0x7E7E было отброшено.
func (h *HeaderReader) Resyncs() int64 { return h.resyncs }

func (h *HeaderReader) frameError(err error) error {
	return &FrameError{Reason: err.Error(), Err: err, Offset: h.consumed}
}

// BytesRead сообщает, сколько байт прочитано из соединения, включая байты,
// отброшенные при повторной синхронизации.
func (h *HeaderReader) BytesRead() int64 { return h.consumed }
