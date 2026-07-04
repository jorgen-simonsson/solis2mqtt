package main

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// Test vectors below are taken verbatim from the Solis Modbus RTU protocol
// document (section 6.1, "Protocol Usage Example").

func mustParseHex(t *testing.T, s string) []byte {
	t.Helper()
	var b []byte
	var hi byte
	have := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		var v byte
		switch {
		case c >= '0' && c <= '9':
			v = c - '0'
		case c >= 'A' && c <= 'F':
			v = c - 'A' + 10
		case c >= 'a' && c <= 'f':
			v = c - 'a' + 10
		default:
			continue
		}
		if !have {
			hi = v
			have = true
		} else {
			b = append(b, hi<<4|v)
			have = false
		}
	}
	return b
}

func TestBuildReadRegistersRequest_MainDSPVersion(t *testing.T) {
	want := mustParseHex(t, "01 04 80 E9 00 01 C9 FE")
	got := buildReadRegistersRequest(0x04, 1, 33001, 1)
	if !bytes.Equal(got, want) {
		t.Fatalf("got % X want % X", got, want)
	}
}

func TestParseReadRegistersResponse_MainDSPVersion(t *testing.T) {
	response := mustParseHex(t, "01 04 02 00 0D 78 F5")
	data, err := parseReadRegistersResponse(1, 0x04, response)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := binary.BigEndian.Uint16(data)
	if got != 0x000D {
		t.Fatalf("got %#04x want %#04x", got, 0x000D)
	}
}

func TestBuildReadRegistersRequest_OverDischargeSOC(t *testing.T) {
	want := mustParseHex(t, "01 03 A8 03 00 01 54 6A")
	got := buildReadRegistersRequest(0x03, 1, 43011, 1)
	if !bytes.Equal(got, want) {
		t.Fatalf("got % X want % X", got, want)
	}
}

func TestParseReadRegistersResponse_OverDischargeSOC(t *testing.T) {
	response := mustParseHex(t, "01 03 02 00 14 B8 4B")
	data, err := parseReadRegistersResponse(1, 0x03, response)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := binary.BigEndian.Uint16(data)
	if got != 20 {
		t.Fatalf("got %d want %d", got, 20)
	}
}

func TestParseReadRegistersResponse_CRCMismatch(t *testing.T) {
	response := mustParseHex(t, "01 04 02 00 0D 00 00")
	if _, err := parseReadRegistersResponse(1, 0x04, response); err == nil {
		t.Fatal("expected CRC mismatch error, got nil")
	}
}

func TestParseReadRegistersResponse_Exception(t *testing.T) {
	// Slave address 1, function 0x04|0x80 (exception), illegal data address.
	frame := []byte{0x01, 0x84, 0x02}
	crc := crc16Modbus(frame)
	frame = append(frame, byte(crc&0xFF), byte(crc>>8))

	if _, err := parseReadRegistersResponse(1, 0x04, frame); err == nil {
		t.Fatal("expected exception error, got nil")
	}
}
