package ndtp

import (
	"encoding/binary"
	"fmt"
	"time"
)

// Идентификаторы ячеек в том виде, в каком они идут по проводу, раздел 7 спецификации.
const (
	CellNav00       byte = 0  // навигация, всегда первая в realtime теле
	CellIntSensor02 byte = 2  // внутренние датчики
	CellUsi08       byte = 8  // датчик уровня топлива UZI-M
	CellCan10       byte = 10 // шина CAN
	CellLls15       byte = 15 // датчик уровня LLS
	CellTermo16     byte = 16 // датчик температуры
	navCellSize          = 26
	cellHeaderSize       = 2 // тип u8 плюс номер u8
)

// cellSizes сопоставляет тип ячейки с фиксированным размером полезной части.
//
// Перечислены только типы, размер которых спецификация задаёт явно: по размерам
// выполняется переход к следующей ячейке, и неверная догадка испортила бы все
// последующие ячейки. Набор покрывает все ячейки, которые генерирует эмулятор
// (Nav00, Usi08, Termo16, IntSensor02, Can10), плюс LLS.
//
// Декодирование на любом другом типе намеренно останавливается, а не гадает;
// см. DecodeCells.
var cellSizes = map[byte]int{
	CellNav00:       navCellSize,
	CellIntSensor02: 26,
	CellUsi08:       6,
	CellCan10:       37,
	CellLls15:       50,
	CellTermo16:     8,
}

// Cell — одна необработанная ячейка из тела realtime. Поле Data ссылается на
// буфер кадра и действительно только до повторного использования кадра.
type Cell struct {
	Type   byte
	Number uint8
	Data   []byte
}

// ErrUnknownCellType — тип ячейки, для которого у декодера нет размера полезной
// части. Длина неизвестна, поэтому пройти дальше по телу нельзя.
type ErrUnknownCellType struct {
	Type byte
}

func (e *ErrUnknownCellType) Error() string {
	return fmt.Sprintf("ndtp: unknown cell type %d, cannot walk past it", e.Type)
}

// ErrTruncatedCell — тело кадра оборвалось посередине ячейки.
type ErrTruncatedCell struct {
	Type   byte
	Want   int
	Got    int
	Offset int
}

func (e *ErrTruncatedCell) Error() string {
	return fmt.Sprintf("ndtp: cell type %d at offset %d truncated: want %d payload bytes, got %d",
		e.Type, e.Offset, e.Want, e.Got)
}

// DecodeCells разбивает тело realtime на ячейки.
//
// G6CellNav00 всегда идёт первой ячейкой, поэтому вызывающий код может полагаться
// на cells[0] как на навигацию даже тогда, когда функция вернула ошибку и
// остановилась на типе ячейки неизвестного размера.
func DecodeCells(body []byte) ([]Cell, error) {
	var cells []Cell
	for offset := 0; offset < len(body); {
		if len(body)-offset < cellHeaderSize {
			return cells, &ErrTruncatedCell{Want: cellHeaderSize, Got: len(body) - offset, Offset: offset}
		}
		cellType := body[offset]
		number := body[offset+1]
		payload := body[offset+cellHeaderSize:]

		size, known := cellSizes[cellType]
		if !known {
			return cells, &ErrUnknownCellType{Type: cellType}
		}
		if len(payload) < size {
			return cells, &ErrTruncatedCell{Type: cellType, Want: size, Got: len(payload), Offset: offset}
		}
		cells = append(cells, Cell{Type: cellType, Number: number, Data: payload[:size]})
		offset += cellHeaderSize + size
	}
	return cells, nil
}

// NavFlags — восемь бит extraDop навигационной ячейки. Биты 0-4 описывают
// состояние устройства, бит 5 — полушарие широты, бит 6 — полушарие долготы,
// бит 7 — достоверность координат.
type NavFlags struct {
	VoiceRequest    bool
	Alarm           bool
	SOS             bool
	FirstPowerOn    bool
	InternalBattery bool
	North           bool
	East            bool
	Valid           bool
}

const (
	dopVoiceRequest    byte = 1 << 0
	dopAlarm           byte = 1 << 1
	dopSOS             byte = 1 << 2
	dopFirstPowerOn    byte = 1 << 3
	dopInternalBattery byte = 1 << 4
	dopNorth           byte = 1 << 5
	dopEast            byte = 1 << 6
	dopValid           byte = 1 << 7
)

// ParseNavFlags распаковывает упакованный байт extraDop. Установленный бит 5
// означает северную широту, бит 6 — восточную долготу, бит 7 — что координатам
// можно доверять.
func ParseNavFlags(b byte) NavFlags {
	return NavFlags{
		VoiceRequest:    b&dopVoiceRequest != 0,
		Alarm:           b&dopAlarm != 0,
		SOS:             b&dopSOS != 0,
		FirstPowerOn:    b&dopFirstPowerOn != 0,
		InternalBattery: b&dopInternalBattery != 0,
		North:           b&dopNorth != 0,
		East:            b&dopEast != 0,
		Valid:           b&dopValid != 0,
	}
}

// NavCell — декодированная запись G6CellNav00, эквивалент одной строки
// traffic.csv на проводе.
type NavCell struct {
	Timestamp  time.Time
	Longitude  float64 // signed degrees
	Latitude   float64 // signed degrees
	Altitude   float64 // metres
	SpeedAvg   float64 // km/h
	SpeedMax   float64 // km/h
	Course     float64 // degrees
	Track      uint16  // metres travelled, mod 65535
	Satellites uint8
	PDOP       uint8
	// BatteryVoltage — напряжение, пересчитанное из единицы 20 мВ.
	BatteryVoltage float64
	Flags          NavFlags
}

// coordScale — фиксированная точка NDTP для долготы и широты.
const coordScale = 1e7

// batteryUnitVolts — напряжение одной сырой единицы batVoltage, то есть 20 мВ.
const batteryUnitVolts = 0.02

// DecodeNavCell декодирует полезную часть G6CellNav00 длиной 26 байт.
func DecodeNavCell(data []byte) (NavCell, error) {
	if len(data) < navCellSize {
		return NavCell{}, &ErrTruncatedCell{Type: CellNav00, Want: navCellSize, Got: len(data)}
	}
	flags := ParseNavFlags(data[12])
	lon := float64(binary.LittleEndian.Uint32(data[4:8])) / coordScale
	lat := float64(binary.LittleEndian.Uint32(data[8:12])) / coordScale
	if !flags.East {
		lon = -lon
	}
	if !flags.North {
		lat = -lat
	}
	return NavCell{
		Timestamp:      time.Unix(int64(binary.LittleEndian.Uint32(data[0:4])), 0).UTC(),
		Longitude:      lon,
		Latitude:       lat,
		Altitude:       float64(binary.LittleEndian.Uint16(data[22:24])),
		SpeedAvg:       float64(binary.LittleEndian.Uint16(data[14:16])),
		SpeedMax:       float64(binary.LittleEndian.Uint16(data[16:18])),
		Course:         float64(binary.LittleEndian.Uint16(data[18:20])),
		Track:          binary.LittleEndian.Uint16(data[20:22]),
		Satellites:     data[24],
		PDOP:           data[25],
		BatteryVoltage: float64(data[13]) * batteryUnitVolts,
		Flags:          flags,
	}, nil
}

// Position возвращает координаты, только если навигационная ячейка отметила их
// достоверными.
//
// Это повторяет поведение traffic.csv, где строки с location_valid=False всё
// равно несут устаревшие или обнулённые координаты; считать их реальной
// позицией нельзя.
func (n NavCell) Position() (lon, lat float64, ok bool) {
	if !n.Flags.Valid {
		return 0, 0, false
	}
	return n.Longitude, n.Latitude, true
}
