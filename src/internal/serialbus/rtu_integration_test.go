//go:build !windows

// This file exercises Bus.Read against a real Modbus RTU wire protocol,
// without any physical serial hardware: github.com/creack/pty gives us a
// connected pair of virtual terminal devices, one of which is opened by
// Bus (exactly as it would open a real /dev/ttyS0), while a hand-rolled
// Modbus RTU slave on the other end answers requests and, in one case,
// misbehaves so we can verify Bus reconnects and recovers on the next call.
package serialbus

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"os"
	"testing"
	"time"

	"github.com/creack/pty"

	"solis2mqtt/src/internal/config"
)

// modbusCRC computes the CRC16 used to frame Modbus RTU messages.
func modbusCRC(data []byte) uint16 {
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

// readRequest reads one fixed-length (8 byte) Modbus RTU read request
// (function code 3 or 4) and returns the requested start address and
// quantity. It is called from background goroutines simulating the slave
// device, so it reports failures with t.Errorf rather than t.Fatalf: only
// the goroutine running the test itself may call FailNow/Fatalf.
func readRequest(t *testing.T, r io.Reader) (addr, qty uint16, ok bool) {
	t.Helper()
	req := make([]byte, 8)
	if _, err := io.ReadFull(r, req); err != nil {
		t.Errorf("read request: %v", err)
		return 0, 0, false
	}
	return binary.BigEndian.Uint16(req[2:4]), binary.BigEndian.Uint16(req[4:6]), true
}

// writeResponse frames and writes a valid Modbus RTU response carrying
// registerBytes for the given slave/function code. See readRequest for why
// it uses t.Errorf instead of t.Fatalf.
func writeResponse(t *testing.T, w io.Writer, slaveID, functionCode byte, registerBytes []byte) {
	t.Helper()
	resp := append([]byte{slaveID, functionCode, byte(len(registerBytes))}, registerBytes...)
	crc := modbusCRC(resp)
	resp = append(resp, byte(crc), byte(crc>>8))
	if _, err := w.Write(resp); err != nil {
		t.Errorf("write response: %v", err)
	}
}

func openTestLink(t *testing.T) (link config.Link, slaveEnd *os.File) {
	t.Helper()
	master, slave, err := pty.Open()
	if err != nil {
		t.Skipf("pty not available in this environment: %v", err)
	}
	t.Cleanup(func() {
		master.Close()
		slave.Close()
	})

	return config.Link{
		LinkID:   "link1",
		LinkType: "modbusRTU",
		LinkName: slave.Name(),
		BaudRate: 9600,
		Parity:   "none",
		DataBits: 8,
		StopBits: 1,
	}, master
}

func TestBus_Read_Integration(t *testing.T) {
	link, master := openTestLink(t)

	done := make(chan struct{})
	go func() {
		defer close(done)
		addr, qty, ok := readRequest(t, master)
		if !ok {
			return
		}
		if addr != 100 || qty != 2 {
			t.Errorf("unexpected request: addr=%d qty=%d", addr, qty)
			return
		}
		writeResponse(t, master, 1, 3, []byte{0x04, 0xD2, 0xFF, 0xFB})
	}()

	mgr := NewManager([]config.Link{link}, 2*time.Second)
	defer mgr.CloseAll()
	bus, ok := mgr.Bus("link1")
	if !ok {
		t.Fatal("expected link1 to be registered")
	}

	data, err := bus.Read(context.Background(), 1, 3, 100, 2)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if want := []byte{0x04, 0xD2, 0xFF, 0xFB}; !bytes.Equal(data, want) {
		t.Errorf("got % x, want % x", data, want)
	}

	<-done
}

func TestBus_Read_RecoversAfterError(t *testing.T) {
	link, master := openTestLink(t)

	mgr := NewManager([]config.Link{link}, 500*time.Millisecond)
	defer mgr.CloseAll()
	bus, _ := mgr.Bus("link1")

	// Round 1: the slave sends back a corrupted frame (bad CRC). Bus.Read
	// must return an error rather than hang or panic.
	go func() {
		if _, _, ok := readRequest(t, master); !ok {
			return
		}
		master.Write([]byte{0x01, 0x03, 0x02, 0xDE, 0xAD, 0x00, 0x00})
	}()

	if _, err := bus.Read(context.Background(), 1, 3, 100, 1); err == nil {
		t.Fatal("expected an error from a corrupted response")
	}

	// Round 2: the slave now answers correctly. This only succeeds if Bus
	// reconnected on its own rather than reusing a broken connection.
	go func() {
		addr, qty, ok := readRequest(t, master)
		if !ok {
			return
		}
		if addr != 100 || qty != 1 {
			t.Errorf("unexpected request: addr=%d qty=%d", addr, qty)
			return
		}
		writeResponse(t, master, 1, 3, []byte{0x00, 0x2A})
	}()

	data, err := bus.Read(context.Background(), 1, 3, 100, 1)
	if err != nil {
		t.Fatalf("Read after recovery: %v", err)
	}
	if want := []byte{0x00, 0x2A}; !bytes.Equal(data, want) {
		t.Errorf("got % x, want % x", data, want)
	}
}
