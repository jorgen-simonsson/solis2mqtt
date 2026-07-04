// Command solistest checks RS485 Modbus RTU connectivity to a Solis
// inverter by reading a single U16 input register (default: 35000,
// the Solis inverter type identifier) and printing the result to stdout.
package main

import (
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"
)

func main() {
	device := flag.String("device", "/dev/ttyS0", "serial device connected to the inverter's RS485 port")
	slaveID := flag.Uint("slave", 1, "Modbus slave address")
	register := flag.Uint("register", 35000, "input register address to read (function code 0x04)")
	timeout := flag.Duration("timeout", 2*time.Second, "response read timeout")
	flag.Parse()

	if *slaveID == 0 || *slaveID > 255 {
		fmt.Fprintln(os.Stderr, "slave address must be between 1 and 255")
		os.Exit(1)
	}
	if *register > 0xFFFF {
		fmt.Fprintln(os.Stderr, "register address must fit in 16 bits")
		os.Exit(1)
	}

	readTimeoutDeciseconds := timeout.Seconds() * 10
	if readTimeoutDeciseconds < 1 {
		readTimeoutDeciseconds = 1
	}
	if readTimeoutDeciseconds > 255 {
		readTimeoutDeciseconds = 255
	}

	port, err := openSerialPort(*device, byte(readTimeoutDeciseconds))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	defer port.Close()

	const functionCode = 0x04 // Read Input Registers
	request := buildReadRegistersRequest(functionCode, byte(*slaveID), uint16(*register), 1)

	fmt.Printf("TX: % X\n", request)

	if _, err := port.Write(request); err != nil {
		fmt.Fprintf(os.Stderr, "error: write failed: %v\n", err)
		os.Exit(1)
	}

	response, err := readResponse(port, *timeout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("RX: % X\n", response)

	if len(response) == 0 {
		fmt.Fprintln(os.Stderr, "error: no response from inverter (timed out)")
		os.Exit(1)
	}

	data, err := parseReadRegistersResponse(byte(*slaveID), functionCode, response)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	value := binary.BigEndian.Uint16(data)
	fmt.Printf("Register %d (U16): %d (0x%04X)\n", *register, value, value)
}

// readResponse accumulates bytes from the serial port until no further
// bytes arrive (VMIN=0 read timeout) or the overall deadline is reached.
func readResponse(port *os.File, overallTimeout time.Duration) ([]byte, error) {
	deadline := time.Now().Add(overallTimeout)
	var response []byte
	buf := make([]byte, 256)

	for time.Now().Before(deadline) {
		n, err := port.Read(buf)
		if n == 0 {
			// A VMIN=0/VTIME read that times out with no bytes available
			// surfaces as io.EOF from os.File.Read; that just means the
			// inverter hasn't sent (more) data yet, not end-of-file.
			if err == nil || errors.Is(err, io.EOF) {
				break
			}
			return response, fmt.Errorf("read failed: %w", err)
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return response, fmt.Errorf("read failed: %w", err)
		}
		response = append(response, buf[:n]...)
	}

	return response, nil
}
