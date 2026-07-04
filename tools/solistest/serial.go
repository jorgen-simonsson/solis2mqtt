package main

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

const (
	tcgets = 0x5401
	tcsets = 0x5402
)

// openSerialPort opens device for RS485 Modbus RTU communication and
// configures it for 9600 8N1, matching the Solis inverter's default
// electrical interface parameters.
func openSerialPort(device string, readTimeoutDeciseconds byte) (*os.File, error) {
	f, err := os.OpenFile(device, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", device, err)
	}

	if err := configureRaw9600_8N1(int(f.Fd()), readTimeoutDeciseconds); err != nil {
		f.Close()
		return nil, fmt.Errorf("configure %s: %w", device, err)
	}

	return f, nil
}

func configureRaw9600_8N1(fd int, readTimeoutDeciseconds byte) error {
	var t syscall.Termios

	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(tcgets), uintptr(unsafe.Pointer(&t))); errno != 0 {
		return errno
	}

	// Raw mode: no input/output processing, no line discipline, no signals.
	t.Iflag &^= syscall.IGNBRK | syscall.BRKINT | syscall.PARMRK | syscall.ISTRIP |
		syscall.INLCR | syscall.IGNCR | syscall.ICRNL | syscall.IXON
	t.Oflag &^= syscall.OPOST
	t.Lflag &^= syscall.ECHO | syscall.ECHONL | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
	t.Cflag &^= syscall.CSIZE | syscall.PARENB | syscall.CSTOPB
	t.Cflag |= syscall.CS8 | syscall.CREAD | syscall.CLOCAL

	// Data Bits: 8, Parity: None, Stop Bits: 1 (per Solis protocol section 2.1).
	t.Ispeed = syscall.B9600
	t.Ospeed = syscall.B9600

	t.Cc[syscall.VMIN] = 0
	t.Cc[syscall.VTIME] = readTimeoutDeciseconds

	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(tcsets), uintptr(unsafe.Pointer(&t))); errno != 0 {
		return errno
	}

	return nil
}
