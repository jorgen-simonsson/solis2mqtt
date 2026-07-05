// Command b23test checks RS485 Modbus RTU connectivity to an ABB B23/B24
// power meter by reading a single U16 holding register (default: 23340,
// 0x5B2C - the meter's line frequency, 0.01 Hz per LSB) and printing the
// result to stdout.
package main

import (
	"context"
	"encoding/binary"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/grid-x/modbus"
)

func main() {
	device := flag.String("device", "/dev/ttyS0", "serial device connected to the meter's RS485 port")
	slaveID := flag.Uint("slave", 1, "Modbus slave address")
	register := flag.Uint("register", 23340, "holding register address to read (function code 0x03)")
	timeout := flag.Duration("timeout", 2*time.Second, "maximum time to wait for a response")
	flag.Parse()

	if *slaveID == 0 || *slaveID > 255 {
		fmt.Fprintln(os.Stderr, "slave address must be between 1 and 255")
		os.Exit(1)
	}
	if *register > 0xFFFF {
		fmt.Fprintln(os.Stderr, "register address must fit in 16 bits")
		os.Exit(1)
	}

	// 9600 8N1 is the ABB B23/B24 meter's default RS485 electrical interface.
	handler := modbus.NewRTUClientHandler(*device)
	handler.BaudRate = 9600
	handler.DataBits = 8
	handler.Parity = "N"
	handler.StopBits = 1
	handler.SlaveID = byte(*slaveID)
	handler.Timeout = *timeout

	ctx := context.Background()
	if err := handler.Connect(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	defer handler.Close()

	client := modbus.NewClient(handler)

	// The ABB meter's Modbus interface only supports function codes 3, 6,
	// and 16 (no 0x04 Read Input Registers).
	data, err := client.ReadHoldingRegisters(ctx, uint16(*register), 1)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	value := binary.BigEndian.Uint16(data)
	fmt.Printf("Register %d (U16): %d (0x%04X) -> %.2f Hz\n", *register, value, value, float64(value)/100)
}
