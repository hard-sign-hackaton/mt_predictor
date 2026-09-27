package ndtp

import (
	"encoding/binary"
	"math"
)

// BuildFrame собирает полный кадр: заголовок NPL, заголовок NPH и тело, причём
// CRC-16/MODBUS от «NPH плюс тело» кладётся в NPL с переставленными байтами.
//
// Кадр помечается как запрос, именно так отправляет эмулятор. Ответ приёмника на
// рукопожатие собирается через buildFrame со сброшенным битом запроса.
//
// Кадры нужны инструменту-фидеру, тестам и для ответа на рукопожатие.
func BuildFrame(peer uint32, serviceID, msgType uint16, requestID uint32, body []byte) []byte {
	return buildFrame(peer, serviceID, msgType, requestID, 1, body)
}

// nphFlagRequest — бит 0 слова флагов NPH.
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
	// frame[4:6] флаги: шифрование, crc, задержка — все нулевые
	binary.LittleEndian.PutUint16(frame[6:8], SwapBytes16(CRC16Modbus(nphAndBody)))
	frame[8] = TypeNPH
	binary.LittleEndian.PutUint32(frame[9:13], peer)
	binary.LittleEndian.PutUint16(frame[13:15], 0) // requestId
	copy(frame[nplSize:], nphAndBody)
	return frame
}

// BuildHandshakeBody собирает 18-байтовое тело NPH_SGC_CONN_REQUEST.
func BuildHandshakeBody(peer uint32) []byte {
	body := make([]byte, handshakeBodySize)
	binary.LittleEndian.PutUint16(body[0:2], protoVersionHigh)
	binary.LittleEndian.PutUint16(body[2:4], protoVersionLow)
	// флаги: шифрование, crc, simulate
	binary.LittleEndian.PutUint32(body[6:10], peer)
	// максимальный размер пакета
	binary.LittleEndian.PutUint32(body[14:18], 0) // reserved
	return body
}

// AppendCell дописывает в dst заголовок ячейки и её полезную часть.
func AppendCell(dst []byte, cellType byte, number uint8, payload []byte) []byte {
	dst = append(dst, cellType, number)
	return append(dst, payload...)
}

// EncodeNavCell кодирует NavCell в 26 байт, которые уходят на провод.
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

// putCoord записывает беззнаковую координату в фиксированной точке. Полушарие
// здесь не кодируется: оно передаётся битами extraDop 5 и 6.
func putCoord(dst []byte, value float64) {
	scaled := uint32(math.Abs(value)*coordScale + 0.5)
	binary.LittleEndian.PutUint32(dst, scaled)
}
