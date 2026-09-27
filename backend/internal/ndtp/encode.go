package ndtp

import (
	"encoding/binary"
	"math"
)

// BuildFrame assembles a complete frame: NPL header, NPH header and body, with
// the CRC-16/MODBUS of "NPH + body" stored byte-swapped in the NPL.
//
// The frame is marked as a request, which is what the emulator sends. The
// receiver's handshake reply uses buildFrame with the request bit cleared.
//
// Frames are produced for the feeder tool, for tests, and to answer a
// handshake.
func BuildFrame(peer uint32, serviceID, msgType uint16, requestID uint32, body []byte) []byte {
	return buildFrame(peer, serviceID, msgType, requestID, 1, body)
}

// nphFlagRequest is bit 0 of the NPH flags word.
const nphFlagRequest uint16 = 1

func buildFrame(peer uint32, serviceID, msgType uint16, requestID uint32, flags uint16, body []byte) []byte {
	nphAndBody := make([]byte, nphSize+len(body))
	binary.LittleEndian.PutUint16(nphAndBody[0:2], serviceID)
	binary.LittleEndian.PutUint16(nphAndBody[2:4], msgType)
	binary.LittleEndian.PutUint16(nphAndBody[4:6], flags)
	binary.LittleEndian.PutUint32(nphAndBody[6:10], requestID)
	copy(nphAndBody[nphSize:], body)

	frame := make([]byte, nplSize+len(nphAndBody))
	binary.LittleEndian.PutUint16(frame[0:2], Signature)
	binary.LittleEndian.PutUint16(frame[2:4], uint16(len(nphAndBody)))
	// frame[4:6] flags: encryption, crc, delay all zero
	binary.LittleEndian.PutUint16(frame[6:8], SwapBytes16(CRC16Modbus(nphAndBody)))
	frame[8] = TypeNPH
	binary.LittleEndian.PutUint32(frame[9:13], peer)
	binary.LittleEndian.PutUint16(frame[13:15], 0) // requestId
	copy(frame[nplSize:], nphAndBody)
	return frame
}

// BuildHandshakeBody builds the 18-byte NPH_SGC_CONN_REQUEST body.
func BuildHandshakeBody(peer uint32) []byte {
	body := make([]byte, handshakeBodySize)
	binary.LittleEndian.PutUint16(body[0:2], protoVersionHigh)
	binary.LittleEndian.PutUint16(body[2:4], protoVersionLow)
	binary.LittleEndian.PutUint16(body[4:6], 0) // flags: encryption, crc, simulate
	binary.LittleEndian.PutUint32(body[6:10], peer)
	binary.LittleEndian.PutUint32(body[10:14], 65535) // maxPacketSize
	binary.LittleEndian.PutUint32(body[14:18], 0)     // reserved
	return body
}

// AppendCell appends a cell header and payload to dst.
func AppendCell(dst []byte, cellType byte, number uint8, payload []byte) []byte {
	dst = append(dst, cellType, number)
	return append(dst, payload...)
}

// EncodeNavCell encodes a NavCell into its 26 wire bytes. Coordinates are
// stored as absolute values scaled by 1e7, with the hemisphere carried in the
// extraDop bits.
func EncodeNavCell(nav NavCell) []byte {
	data := make([]byte, navCellSize)
	binary.LittleEndian.PutUint32(data[0:4], uint32(nav.Timestamp.Unix()))
	putCoord(data[4:8], nav.Longitude)
	putCoord(data[8:12], nav.Latitude)

	var dop byte
	if nav.Flags.VoiceRequest {
		dop |= dopVoiceRequest
	}
	if nav.Flags.Alarm {
		dop |= dopAlarm
	}
	if nav.Flags.SOS {
		dop |= dopSOS
	}
	if nav.Flags.FirstPowerOn {
		dop |= dopFirstPowerOn
	}
	if nav.Flags.InternalBattery {
		dop |= dopInternalBattery
	}
	if nav.Flags.North {
		dop |= dopNorth
	}
	if nav.Flags.East {
		dop |= dopEast
	}
	if nav.Flags.Valid {
		dop |= dopValid
	}
	data[12] = dop
	data[13] = byte(nav.BatteryVoltage / batteryUnitVolts)
	binary.LittleEndian.PutUint16(data[14:16], uint16(nav.SpeedAvg))
	binary.LittleEndian.PutUint16(data[16:18], uint16(nav.SpeedMax))
	binary.LittleEndian.PutUint16(data[18:20], uint16(nav.Course))
	binary.LittleEndian.PutUint16(data[20:22], nav.Track)
	binary.LittleEndian.PutUint16(data[22:24], uint16(nav.Altitude))
	data[24] = nav.Satellites
	data[25] = nav.PDOP
	return data
}

// putCoord writes the unsigned fixed-point coordinate. The hemisphere is not
// encoded here: it travels in extraDopBit5/extraDopBit6.
func putCoord(dst []byte, value float64) {
	scaled := uint32(math.Abs(value)*coordScale + 0.5)
	binary.LittleEndian.PutUint32(dst, scaled)
}
