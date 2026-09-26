package ndtp

import (
	"encoding/binary"
	"fmt"
	"time"
)

// Cell identifiers as used on the wire (see the specification, §7).
const (
	CellNav00       byte = 0  // navigation, always first in a realtime body
	CellIntSensor02 byte = 2  // internal sensors
	CellUsi08       byte = 8  // UZI-M fuel level sensor
	CellCan10       byte = 10 // CAN bus
	CellLls15       byte = 15 // LLS level gauge
	CellTermo16     byte = 16 // temperature probe
	navCellSize          = 26
	cellHeaderSize       = 2 // type u8 + number u8
)

// cellSizes maps a cell type to its fixed payload size in bytes.
//
// Only types whose size the specification states explicitly are listed: sizes
// are needed to walk to the *next* cell, and a wrong guess would corrupt every
// cell after it. This set covers the whole cell set the emulator generates
// (Nav00, Usi08, Termo16, IntSensor02, Can10) plus LLS.
//
// Decoding deliberately stops at any other type rather than guessing; see
// DecodeCells.
var cellSizes = map[byte]int{
	CellNav00:       navCellSize,
	CellIntSensor02: 26,
	CellUsi08:       6,
	CellCan10:       37,
	CellLls15:       50,
	CellTermo16:     8,
}

// Cell is one raw cell from a realtime body. Data aliases the frame buffer and
// is only valid until the frame is reused.
type Cell struct {
	Type   byte
	Number uint8
	Data   []byte
}

// ErrUnknownCellType reports a cell type this decoder has no payload size for.
// Because the payload length is unknown, the rest of the body cannot be walked.
type ErrUnknownCellType struct {
	Type byte
}

func (e *ErrUnknownCellType) Error() string {
	return fmt.Sprintf("ndtp: unknown cell type %d, cannot walk past it", e.Type)
}

// ErrTruncatedCell reports a body that ended in the middle of a cell.
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

// DecodeCells splits a realtime body into its cells.
//
// G6CellNav00 is always the first cell of a realtime body, so a caller can rely
// on cells[0] being navigation even when this function reports a non-nil error
// and stops early at a cell type of unknown size.
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

// NavFlags holds the eight extraDop bits of a navigation cell. Bits 0-4 are
// device state, bit5 the latitude hemisphere, bit6 the longitude hemisphere and
// bit7 coordinate validity.
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

// ParseNavFlags unpacks the packed extraDop byte. Bit 5 set means northern
// latitude, bit 6 eastern longitude, bit 7 that the coordinates are trustworthy.
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

// NavCell is a decoded G6CellNav00 record, the wire equivalent of one
// traffic.csv row.
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
	// BatteryVoltage is decoded from the raw unit where 1 unit = 20 mV.
	BatteryVoltage float64
	Flags          NavFlags
}

// coordScale is the NDTP fixed-point scale for longitude and latitude.
const coordScale = 1e7

// batteryUnitVolts is the voltage of one raw batVoltage unit (20 mV).
const batteryUnitVolts = 0.02

// DecodeNavCell decodes a 26-byte G6CellNav00 payload.
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

// Position returns the coordinates only when the navigation cell marked them
// valid.
//
// This mirrors traffic.csv, where rows with location_valid=False still carry
// coordinates that are stale or zeroed; callers must not treat those as a real
// position.
func (n NavCell) Position() (lon, lat float64, ok bool) {
	if !n.Flags.Valid {
		return 0, 0, false
	}
	return n.Longitude, n.Latitude, true
}
