package main

import (
	"encoding/binary"
	"fmt"
)

// crc16Modbus computes the Modbus RTU CRC16 (poly 0xA001, init 0xFFFF).
func crc16Modbus(data []byte) uint16 {
	crc := uint16(0xFFFF)
	for _, b := range data {
		crc ^= uint16(b)
		for i := 0; i < 8; i++ {
			if crc&1 != 0 {
				crc = (crc >> 1) ^ 0xA001
			} else {
				crc >>= 1
			}
		}
	}
	return crc
}

// buildReadRegistersRequest builds a Modbus RTU request frame for reading
// registers via the given function code (0x03 Read Holding Registers or
// 0x04 Read Input Registers).
func buildReadRegistersRequest(functionCode byte, slaveID byte, register uint16, count uint16) []byte {
	frame := make([]byte, 6)
	frame[0] = slaveID
	frame[1] = functionCode
	binary.BigEndian.PutUint16(frame[2:4], register)
	binary.BigEndian.PutUint16(frame[4:6], count)

	crc := crc16Modbus(frame)
	return append(frame, byte(crc&0xFF), byte(crc>>8))
}

// parseReadRegistersResponse validates CRC and extracts the register data
// bytes from a Modbus RTU response to a 0x03/0x04 read request.
func parseReadRegistersResponse(slaveID byte, functionCode byte, response []byte) ([]byte, error) {
	if len(response) < 5 {
		return nil, fmt.Errorf("response too short (%d bytes)", len(response))
	}

	gotCRC := binary.LittleEndian.Uint16(response[len(response)-2:])
	wantCRC := crc16Modbus(response[:len(response)-2])
	if gotCRC != wantCRC {
		return nil, fmt.Errorf("CRC mismatch: got %04X want %04X", gotCRC, wantCRC)
	}

	if response[0] != slaveID {
		return nil, fmt.Errorf("unexpected slave address: got %d want %d", response[0], slaveID)
	}

	if response[1] == functionCode|0x80 {
		return nil, fmt.Errorf("inverter returned exception code 0x%02X", response[2])
	}
	if response[1] != functionCode {
		return nil, fmt.Errorf("unexpected function code: got 0x%02X want 0x%02X", response[1], functionCode)
	}

	byteCount := int(response[2])
	if len(response) < 3+byteCount+2 {
		return nil, fmt.Errorf("response byte count %d exceeds frame length", byteCount)
	}

	return response[3 : 3+byteCount], nil
}
