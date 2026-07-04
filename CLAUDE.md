# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project purpose

solis2mqtt reads data from a Solis hybrid inverter over RS485 Modbus RTU and (eventually) publishes it to an MQTT broker. The project is in an early stage: only a standalone Modbus connectivity-test CLI exists so far (`tools/solistest`). There is no MQTT client, no daemon/service entry point, and no configuration file format yet — don't assume these exist when navigating the code.

The authoritative reference for register addresses, data types, scaling factors, and function codes is the vendor protocol document at `doc/RS485_MODBUS(ESINV-33000ID) Hybrid Inverter.pdf`. Any new register support should be cross-checked against it rather than guessed.

## Commands

Requires Go 1.23+ (see `go.mod`, module `solis2mqtt`). If `go` is not on PATH and there's no sudo access, download the official tarball from https://go.dev/dl/ and extract it into a user-writable directory (e.g. `~/go-sdk`), then add its `bin/` to PATH — no root required.

```bash
go build ./...                                    # build everything
go vet ./...                                      # static checks
gofmt -l .                                        # formatting check (should print nothing)
go test ./...                                      # run all tests
go test ./tools/solistest -run TestName -v         # run a single test
go run ./tools/solistest -device /dev/ttyS0        # run the connectivity CLI without a build step
```

`tools/solistest` has zero external dependencies by design (stdlib only), so it builds without network access or `go mod download` — important since it's meant to run on the same embedded/RPi-class device connected to the inverter's RS485 port, which may be offline.

## Architecture

- **`tools/solistest/modbus.go`** — pure Modbus RTU framing: CRC16/Modbus checksum, request builder, response parser/validator. No I/O. Tested in `modbus_test.go` against byte-for-byte request/response examples taken directly from the protocol PDF's worked examples (section 6.1), so these tests double as protocol conformance checks.
- **`tools/solistest/serial.go`** — configures the serial fd for raw 9600/8N1 (the inverter's default RS485 parameters) via direct `termios` ioctl syscalls, deliberately avoiding `golang.org/x/sys/unix` to keep the tool dependency-free.
- **`tools/solistest/main.go`** — CLI flags (`-device`, `-slave`, `-register`, `-timeout`) and orchestration: send request, read response, decode as U16.

Two non-obvious protocol/platform details future changes should preserve:
1. **Register addressing has no offset.** The decimal register addresses in the PDF map directly to the raw big-endian address field on the wire (e.g. register 33001 → request bytes `80 E9`). Don't apply the usual Modbus `40001`-style offset conventions here.
2. **Serial read timeout surfaces as `io.EOF`, not `(0, nil)`.** With `VMIN=0`/`VTIME>0` termios settings, a read that times out with no data returns 0 bytes at the syscall level, but Go's `os.File.Read` turns that specific case into `io.EOF`. Code that reads from the serial port must treat `io.EOF` as "no more data right now," not as a fatal/EOF-of-stream error.
