package ndtp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The canonical CRC-16/MODBUS check value: CRC of "123456789" is 0x4B37.
func TestCRC16ModbusCheckValue(t *testing.T) {
	assert.Equal(t, uint16(0x4B37), CRC16Modbus([]byte("123456789")))
}

// crc16ModbusBitwise is a deliberately naive, table-free reference used to
// cross-check the table-driven implementation.
func crc16ModbusBitwise(data []byte) uint16 {
	crc := uint16(0xFFFF)
	for _, b := range data {
		crc ^= uint16(b)
		for range 8 {
			if crc&1 != 0 {
				crc = (crc >> 1) ^ 0xA001
			} else {
				crc >>= 1
			}
		}
	}
	return crc
}

func TestCRC16ModbusMatchesBitwiseReference(t *testing.T) {
	inputs := [][]byte{
		{},
		{0x00},
		{0xFF},
		{0x7E, 0x7E},
		[]byte("NDTP frame body"),
		make([]byte, 256),
	}
	for i := range 256 {
		inputs[len(inputs)-1][i] = byte(i * 7)
	}
	for _, in := range inputs {
		assert.Equal(t, crc16ModbusBitwise(in), CRC16Modbus(in), "input %x", in)
	}
}

func TestCRC16ModbusEmptyIsInitValue(t *testing.T) {
	assert.Equal(t, uint16(0xFFFF), CRC16Modbus(nil))
}

func TestCRC16ModbusDetectsSingleBitFlips(t *testing.T) {
	base := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06}
	want := CRC16Modbus(base)
	for i := range base {
		for bit := range 8 {
			corrupted := append([]byte(nil), base...)
			corrupted[i] ^= 1 << bit
			assert.NotEqual(t, want, CRC16Modbus(corrupted), "flip bit %d of byte %d", bit, i)
		}
	}
}

func TestSwapBytes16(t *testing.T) {
	assert.Equal(t, uint16(0x374B), SwapBytes16(0x4B37))
	assert.Equal(t, uint16(0x4B37), SwapBytes16(SwapBytes16(0x4B37)))
	assert.Equal(t, uint16(0x0000), SwapBytes16(0x0000))
	assert.Equal(t, uint16(0xFFFF), SwapBytes16(0xFFFF))
	assert.Equal(t, uint16(0x0001), SwapBytes16(0x0100))
}

func TestSwapBytes16IsItsOwnInverse(t *testing.T) {
	for _, v := range []uint16{0, 1, 0x00FF, 0xFF00, 0x1234, 0x7E7E, 0xA001, 0xFFFF} {
		require.Equal(t, v, SwapBytes16(SwapBytes16(v)), "value %#04x", v)
	}
}
