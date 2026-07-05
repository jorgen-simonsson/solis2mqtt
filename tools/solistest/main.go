// Command solistest checks RS485 Modbus RTU connectivity to a Solis
// inverter by reading a single U16 input register (default: 35000,
// the Solis inverter type identifier) and printing the result to stdout.
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
	device := flag.String("device", "/dev/ttyS0", "serial device connected to the inverter's RS485 port")
	slaveID := flag.Uint("slave", 1, "Modbus slave address")
	register := flag.Uint("register", 35000, "input register address to read (function code 0x04)")
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

	// 9600 8N1 is the Solis inverter's default RS485 electrical interface.
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

	data, err := client.ReadInputRegisters(ctx, uint16(*register), 1)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	value := binary.BigEndian.Uint16(data)
	fmt.Printf("Register %d (U16): %d (0x%04X)\n", *register, value, value)
}
