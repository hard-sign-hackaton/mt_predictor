// Package ndtp implements the receiver side of the NDTP ("GrAnit") telematics
// wire protocol: framing, CRC validation and telemetry cell decoding.
//
// Layout of a single frame, all fields little-endian, structures packed:
//
//	[ NPL 15 bytes ][ NPH 10 bytes ][ body ]
//
// See docs/Emulator-and-Telematic-Packets-Specification.md.
package ndtp

// CRC-16/MODBUS parameters: polynomial 0xA001 (reversed 0x8005), init 0xFFFF,
// no reflection of the input, no final XOR.
const (
	crcInit = 0xFFFF
	crcPoly = 0xA001
	// crcTableBits is how many times a table entry is shifted while being
	// built: one iteration per bit of the byte index.
	crcTableBits = 8
)

// crcTable is the standard MODBUS lookup table, built once at init.
var crcTable = buildCRCTable()

func buildCRCTable() [256]uint16 {
	var table [256]uint16
	for i := range table {
		crc := uint16(i)
		for range crcTableBits {
			if crc&1 != 0 {
				crc = (crc >> 1) ^ crcPoly
			} else {
				crc >>= 1
			}
		}
		table[i] = crc
	}
	return table
}

// CRC16Modbus returns the CRC-16/MODBUS checksum of data.
//
// NDTP stores this value in the NPL header with its two bytes swapped, so
// callers comparing against the wire value need SwapBytes16.
func CRC16Modbus(data []byte) uint16 {
	crc := uint16(crcInit)
	for _, b := range data {
		crc = (crc >> 8) ^ crcTable[byte(crc)^b]
	}
	return crc
}

// SwapBytes16 returns v with its two bytes exchanged. The NPL header carries
// the frame CRC in this swapped form.
func SwapBytes16(v uint16) uint16 {
	return v>>8 | v<<8
}
