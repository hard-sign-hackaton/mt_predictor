package ndtp

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rawNav assembles a 26-byte G6CellNav00 payload field by field, independently
// of the package encoder, so decoding is checked against the specification
// rather than against our own writer.
func rawNav(ts uint32, lon, lat uint32, dop byte, bat uint8, speedAvg, speedMax, course, track, alt uint16, nsat, pdop uint8) []byte {
	d := make([]byte, navCellSize)
	put32(d[0:4], ts)
	put32(d[4:8], lon)
	put32(d[8:12], lat)
	d[12] = dop
	d[13] = bat
	put16(d[14:16], speedAvg)
	put16(d[16:18], speedMax)
	put16(d[18:20], course)
	put16(d[20:22], track)
	put16(d[22:24], alt)
	d[24] = nsat
	d[25] = pdop
	return d
}

func put16(b []byte, v uint16) { b[0] = byte(v); b[1] = byte(v >> 8) }
func put32(b []byte, v uint32) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
}

func TestDecodeNavCellMatchesSpecificationExample(t *testing.T) {
	// The example from the specification: 376173210, 557551234 with
	// extraDopBit5/6/7 set is 55.7551234 N, 37.6173210 E in Moscow.
	dop := dopNorth | dopEast | dopValid
	nav, err := DecodeNavCell(rawNav(1767665400, 376173210, 557551234, dop, 250, 42, 55, 90, 1234, 156, 9, 2))
	require.NoError(t, err)

	assert.InDelta(t, 37.6173210, nav.Longitude, 1e-7)
	assert.InDelta(t, 55.7551234, nav.Latitude, 1e-7)
	assert.True(t, nav.Flags.Valid)
	assert.True(t, nav.Flags.North)
	assert.True(t, nav.Flags.East)
	assert.Equal(t, 42.0, nav.SpeedAvg)
	assert.Equal(t, 55.0, nav.SpeedMax)
	assert.Equal(t, 90.0, nav.Course)
	assert.Equal(t, uint16(1234), nav.Track)
	assert.Equal(t, 156.0, nav.Altitude)
	assert.Equal(t, uint8(9), nav.Satellites)
	assert.Equal(t, uint8(2), nav.PDOP)
	assert.Equal(t, time.Unix(1767665400, 0).UTC(), nav.Timestamp)
	// 250 units * 20 mV = 5.0 V
	assert.InDelta(t, 5.0, nav.BatteryVoltage, 1e-9)
}

func TestDecodeNavCellAppliesHemisphereSign(t *testing.T) {
	tests := []struct {
		name    string
		dop     byte
		wantLon float64
		wantLat float64
	}{
		{"north east", dopNorth | dopEast, 37.6173210, 55.7551234},
		{"south west", 0, -37.6173210, -55.7551234},
		{"south east", dopEast, 37.6173210, -55.7551234},
		{"north west", dopNorth, -37.6173210, 55.7551234},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			nav, err := DecodeNavCell(rawNav(1, 376173210, 557551234, tc.dop, 0, 0, 0, 0, 0, 0, 0, 0))
			require.NoError(t, err)
			assert.InDelta(t, tc.wantLon, nav.Longitude, 1e-7)
			assert.InDelta(t, tc.wantLat, nav.Latitude, 1e-7)
		})
	}
}

func TestParseNavFlagsDeviceBits(t *testing.T) {
	all := ParseNavFlags(0xFF)
	assert.True(t, all.VoiceRequest)
	assert.True(t, all.Alarm)
	assert.True(t, all.SOS)
	assert.True(t, all.FirstPowerOn)
	assert.True(t, all.InternalBattery)
	assert.True(t, all.North)
	assert.True(t, all.East)
	assert.True(t, all.Valid)

	none := ParseNavFlags(0x00)
	assert.False(t, none.VoiceRequest)
	assert.False(t, none.Alarm)
	assert.False(t, none.SOS)
	assert.False(t, none.FirstPowerOn)
	assert.False(t, none.InternalBattery)
	assert.False(t, none.North)
	assert.False(t, none.East)
	assert.False(t, none.Valid)

	// extraDopBit0 is the least significant bit, so bit 7 alone is 0x80.
	onlyValid := ParseNavFlags(0x80)
	assert.True(t, onlyValid.Valid)
	assert.False(t, onlyValid.North)
	assert.False(t, onlyValid.East)

	// The specification's "synthetic navigation" case: zero coordinates with
	// N/E/valid set, which is what the emulator injects when a config omits
	// G6CellNav00.
	synthetic := mustDecode(t, rawNav(0, 0, 0, dopNorth|dopEast|dopValid, 0, 0, 0, 0, 0, 0, 0, 0))
	assert.True(t, synthetic.Flags.Valid)
	assert.Zero(t, synthetic.Longitude)
	assert.Zero(t, synthetic.Latitude)
}

func mustDecode(t *testing.T, data []byte) NavCell {
	t.Helper()
	nav, err := DecodeNavCell(data)
	require.NoError(t, err)
	return nav
}

func TestNavCellPositionHonoursValidity(t *testing.T) {
	valid := mustDecode(t, rawNav(1, 376173210, 557551234, dopNorth|dopEast|dopValid, 0, 0, 0, 0, 0, 0, 0, 0))
	lon, lat, ok := valid.Position()
	assert.True(t, ok)
	assert.InDelta(t, 37.6173210, lon, 1e-7)
	assert.InDelta(t, 55.7551234, lat, 1e-7)

	// An invalid cell may still carry coordinates, exactly like the
	// location_valid=False rows of traffic.csv. Position must refuse them.
	invalid := mustDecode(t, rawNav(1, 376173210, 557551234, dopNorth|dopEast, 0, 0, 0, 0, 0, 0, 0, 0))
	lon, lat, ok = invalid.Position()
	assert.False(t, ok)
	assert.Zero(t, lon)
	assert.Zero(t, lat)
}

func TestDecodeNavCellRejectsShortPayload(t *testing.T) {
	_, err := DecodeNavCell(make([]byte, navCellSize-1))
	require.Error(t, err)

	var typed *ErrTruncatedCell
	require.ErrorAs(t, err, &typed)
	assert.Equal(t, navCellSize, typed.Want)
	assert.Equal(t, navCellSize-1, typed.Got)
}

func TestEncodeDecodeNavCellRoundTrip(t *testing.T) {
	original := NavCell{
		Timestamp:      time.Unix(1767665400, 0).UTC(),
		Longitude:      37.617321,
		Latitude:       55.7551234,
		Altitude:       156,
		SpeedAvg:       42,
		SpeedMax:       55,
		Course:         359,
		Track:          65500,
		Satellites:     12,
		PDOP:           3,
		BatteryVoltage: 4.2,
		Flags:          NavFlags{North: true, East: true, Valid: true, Alarm: true},
	}
	decoded := mustDecode(t, EncodeNavCell(original))
	assert.Equal(t, original.Timestamp, decoded.Timestamp)
	assert.InDelta(t, original.Longitude, decoded.Longitude, 1e-7)
	assert.InDelta(t, original.Latitude, decoded.Latitude, 1e-7)
	assert.Equal(t, original.Altitude, decoded.Altitude)
	assert.Equal(t, original.SpeedAvg, decoded.SpeedAvg)
	assert.Equal(t, original.SpeedMax, decoded.SpeedMax)
	assert.Equal(t, original.Course, decoded.Course)
	assert.Equal(t, original.Track, decoded.Track)
	assert.Equal(t, original.Satellites, decoded.Satellites)
	assert.Equal(t, original.PDOP, decoded.PDOP)
	assert.InDelta(t, original.BatteryVoltage, decoded.BatteryVoltage, 1e-9)
	assert.Equal(t, original.Flags, decoded.Flags)
}

func TestEncodeDecodeNavCellRoundTripSouthernWestern(t *testing.T) {
	original := NavCell{
		Timestamp: time.Unix(1, 0).UTC(),
		Longitude: -73.98,
		Latitude:  -33.86,
		Flags:     NavFlags{Valid: true},
	}
	decoded := mustDecode(t, EncodeNavCell(original))
	assert.InDelta(t, -73.98, decoded.Longitude, 1e-7)
	assert.InDelta(t, -33.86, decoded.Latitude, 1e-7)
	assert.False(t, decoded.Flags.North)
	assert.False(t, decoded.Flags.East)
}

func TestDecodeCellsWalksKnownCells(t *testing.T) {
	nav := rawNav(1767665400, 376173210, 557551234, dopNorth|dopEast|dopValid, 250, 42, 55, 90, 0, 0, 9, 2)
	usi := make([]byte, 6)   // G6CellUsi08
	termo := make([]byte, 8) // G6CellTermo16

	body := AppendCell(nil, CellNav00, 0, nav)
	body = AppendCell(body, CellUsi08, 0, usi)
	body = AppendCell(body, CellUsi08, 1, usi) // a second fuel sensor
	body = AppendCell(body, CellTermo16, 0, termo)

	cells, err := DecodeCells(body)
	require.NoError(t, err)
	require.Len(t, cells, 4)

	// Nav00 must be first: that guarantee is what lets the receiver keep
	// working even when a later cell type is unknown.
	assert.Equal(t, CellNav00, cells[0].Type)
	assert.Equal(t, uint8(0), cells[0].Number)
	assert.Len(t, cells[0].Data, navCellSize)

	assert.Equal(t, CellUsi08, cells[1].Type)
	assert.Equal(t, uint8(0), cells[1].Number)
	assert.Equal(t, CellUsi08, cells[2].Type)
	assert.Equal(t, uint8(1), cells[2].Number)
	assert.Equal(t, CellTermo16, cells[3].Type)
}

func TestDecodeCellsStopsAtUnknownTypeButKeepsNav(t *testing.T) {
	nav := rawNav(1, 376173210, 557551234, dopNorth|dopEast|dopValid, 0, 0, 0, 0, 0, 0, 0, 0)
	body := AppendCell(nil, CellNav00, 0, nav)
	body = AppendCell(body, 99, 0, make([]byte, 12)) // type 99: no known size

	cells, err := DecodeCells(body)
	require.Error(t, err)

	var typed *ErrUnknownCellType
	require.ErrorAs(t, err, &typed)
	assert.Equal(t, byte(99), typed.Type)

	require.Len(t, cells, 1)
	assert.Equal(t, CellNav00, cells[0].Type)
}

func TestDecodeCellsReportsTruncation(t *testing.T) {
	nav := rawNav(1, 1, 1, dopValid, 0, 0, 0, 0, 0, 0, 0, 0)
	body := AppendCell(nil, CellNav00, 0, nav)
	body = append(body, CellUsi08, 0, 0x00, 0x00) // needs 6 bytes, has 2

	cells, err := DecodeCells(body)
	require.Error(t, err)

	var typed *ErrTruncatedCell
	require.ErrorAs(t, err, &typed)
	assert.Equal(t, CellUsi08, typed.Type)
	assert.Equal(t, 6, typed.Want)
	assert.Equal(t, 2, typed.Got)
	require.Len(t, cells, 1)
}

func TestDecodeCellsRejectsShortHeader(t *testing.T) {
	_, err := DecodeCells([]byte{CellNav00})
	require.Error(t, err)

	var typed *ErrTruncatedCell
	require.ErrorAs(t, err, &typed)
	assert.Equal(t, cellHeaderSize, typed.Want)
}

func TestDecodeCellsEmptyBody(t *testing.T) {
	cells, err := DecodeCells(nil)
	assert.NoError(t, err)
	assert.Empty(t, cells)
}

func TestCellSizesCoverEmulatorAutoGenerateSet(t *testing.T) {
	// The emulator's autoGenerate set is Nav00, Usi08, Termo16, IntSensor02
	// and Can10. All must be walkable or a real stream would stop after nav.
	for _, cellType := range []byte{CellNav00, CellUsi08, CellTermo16, CellIntSensor02, CellCan10} {
		_, known := cellSizes[cellType]
		assert.True(t, known, "cell type %d missing from cellSizes", cellType)
	}
}
