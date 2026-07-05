# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project purpose

solis2mqtt reads data from a Solis hybrid inverter and an ABB B23/B24 power meter, both over RS485 Modbus RTU, and (eventually) publishes it to an MQTT broker. The project is in an early stage: only standalone Modbus connectivity-test CLIs exist so far (`tools/solistest` for the inverter, `tools/b23test` for the meter). There is no MQTT client, no daemon/service entry point, and no configuration file format yet — don't assume these exist when navigating the code.

The authoritative references for register addresses, data types, scaling factors, and function codes are the vendor protocol documents: `doc/RS485_MODBUS(ESINV-33000ID) Hybrid Inverter.pdf` for the Solis inverter, and `doc/B23_B24_User_Manual.pdf.pdf` (chapter 9, "Communication with Modbus") for the ABB meter. Any new register support should be cross-checked against the relevant document rather than guessed.

## Commands

Requires Go 1.23+ (see `go.mod`, module `solis2mqtt`). If `go` is not on PATH and there's no sudo access, download the official tarball from https://go.dev/dl/ and extract it into a user-writable directory (e.g. `~/go-sdk`), then add its `bin/` to PATH — no root required.

```bash
go build ./...                                    # build everything
go vet ./...                                      # static checks
gofmt -l .                                        # formatting check (should print nothing)
go test ./...                                      # run all tests (none currently exist; Modbus framing/serial I/O is delegated to grid-x/modbus)
go run ./tools/solistest -device /dev/ttyS0        # run the inverter connectivity CLI without a build step
go run ./tools/b23test -device /dev/ttyS0          # run the ABB meter connectivity CLI without a build step
```

## Architecture

Both connectivity-test CLIs are single-file `main.go` programs built on [`github.com/grid-x/modbus`](https://github.com/grid-x/modbus) (a fork of the historically-standard `goburrow/modbus`, actively maintained and BSD-3-Clause licensed), which handles Modbus RTU framing (CRC16, request/response encode-decode, exception handling) and the underlying serial port (via its `github.com/grid-x/serial` dependency) internally. There is no hand-rolled framing or termios code in this repo anymore — don't reintroduce it; extend via the library's `Client`/`RTUClientHandler` API instead.

- **`tools/solistest/main.go`** — CLI flags (`-device`, `-slave`, `-register`, `-timeout`), configures a `modbus.NewRTUClientHandler` for 9600 8N1, and reads one input register (function code 0x04, default 35000 — the Solis inverter type identifier) via `client.ReadInputRegisters`.
- **`tools/b23test/main.go`** — same structure, targeting the ABB B23/B24 power meter. Reads one holding register (function code 0x03, default 23340 / 0x5B2C — line frequency, 0.01 Hz/LSB) via `client.ReadHoldingRegisters`. The ABB meter's Modbus interface only supports function codes 3, 6, and 16 (no 0x04 Read Input Registers), unlike the Solis inverter.

The two tools still don't share a package — each is a self-contained ~60-line `main.go` — but nothing prevents factoring out common handler setup into an internal package if a third tool or the eventual MQTT daemon needs it.

One non-obvious protocol detail future changes should preserve:
**Register addressing has no offset.** The decimal register addresses in the vendor PDFs map directly to the address passed to `ReadInputRegisters`/`ReadHoldingRegisters` (e.g. Solis register 33001 → address `33001`, wire bytes `80 E9`). Don't apply the usual Modbus `40001`-style offset conventions here. This holds for both the Solis and ABB register maps.
