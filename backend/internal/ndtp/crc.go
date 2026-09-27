// Package ndtp реализует приёмную часть протокола телематики NDTP: разбор
// кадров, проверку CRC и декодирование ячеек телеметрии.
//
// Устройство одного кадра, все поля little-endian, структуры без выравнивания:
//
//	[ NPL 15 байт ][ NPH 10 байт ][ тело ]
//
// Подробности протокола: docs/Emulator-and-Telematic-Packets-Specification.md.
package ndtp

// Параметры CRC-16/MODBUS: полином 0xA001 (перевёрнутый 0x8005), инициализация
// 0xFFFF, без отражения входа и без финального XOR.
const (
	crcInit = 0xFFFF
	crcPoly = 0xA001
	// crcTableBits — число сдвигов при построении элемента таблицы, по одному
	// на каждый бит индекса байта.
	crcTableBits = 8
)

// crcTable — стандартная таблица MODBUS, строится один раз при инициализации.
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

// CRC16Modbus возвращает контрольную сумму CRC-16/MODBUS для данных.
//
// В заголовке NPL это значение хранится с переставленными байтами, поэтому при
// сравнении с байтами на проводе нужен SwapBytes16.
func CRC16Modbus(data []byte) uint16 {
	crc := uint16(crcInit)
	for _, b := range data {
		crc = (crc >> 8) ^ crcTable[byte(crc)^b]
	}
	return crc
}

// SwapBytes16 меняет местами два байта значения. В таком виде заголовок NPL
// хранит CRC кадра.
func SwapBytes16(v uint16) uint16 {
	return v>>8 | v<<8
}
