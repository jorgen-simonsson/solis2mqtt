# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project purpose

solis2mqtt reads data from a Solis hybrid inverter and an ABB B23/B24 power meter, both over RS485 Modbus RTU, and publishes it as JSON to an MQTT broker on a repeating schedule. Which devices, links, registers and MQTT topics to use is entirely data-driven via `config.json` (see `doc/config_template.json` for the schema) — adding a new register or device should not require code changes.

The authoritative references for register addresses, data types, scaling factors, and function codes are the vendor protocol documents: `doc/RS485_MODBUS(ESINV-33000ID) Hybrid Inverter.pdf` for the Solis inverter, and `doc/B23_B24_User_Manual.pdf.pdf` (chapter 9, "Communication with Modbus") for the ABB meter. Any new register support should be cross-checked against the relevant document rather than guessed.

Two independent things live in this repo:

- **`tools/solistest` and `tools/b23test`** — standalone, single-register Modbus connectivity-test CLIs (predate the daemon, kept for ad-hoc debugging).
- **`src/`** — the actual daemon: reads `config.json`, polls all configured devices/registers over one or more serial links, and publishes JSON payloads to MQTT.

## Commands

Requires Go 1.24+ (see `go.mod`, module `solis2mqtt`). If `go` is not on PATH and there's no sudo access, download the official tarball from https://go.dev/dl/ and extract it into a user-writable directory (e.g. `~/go-sdk`), then add its `bin/` to PATH — no root required.

```bash
go build ./...                                            # build everything
go vet ./...                                              # static checks
gofmt -l .                                                # formatting check (should print nothing)
go test ./...                                              # run all tests (src/internal/*, table-driven + integration)
go run ./tools/solistest -device /dev/ttyS0                # inverter connectivity CLI, no build step
go run ./tools/b23test -device /dev/ttyS0                  # ABB meter connectivity CLI, no build step
go run ./src/cmd/solis2mqtt -config src/config/config.json -env src/.env   # run the daemon directly
```

To run the daemon in Docker (build context is the repo root, since `go.mod`/`go.sum` live there):

```bash
cd src
cp .env.example .env   # fill in MQTT_HOST/MQTT_USERNAME/MQTT_PASSWORD etc.
# edit config/config.json to match your devices, links and register tables
docker compose up --build
```

## Architecture

### `tools/solistest` and `tools/b23test`

Single-file `main.go` programs built on [`github.com/grid-x/modbus`](https://github.com/grid-x/modbus) (a fork of the historically-standard `goburrow/modbus`, actively maintained and BSD-3-Clause licensed), which handles Modbus RTU framing (CRC16, request/response encode-decode, exception handling) and the underlying serial port (via its `github.com/grid-x/serial` dependency) internally. There is no hand-rolled framing or termios code in this repo — don't reintroduce it; extend via the library's `Client`/`RTUClientHandler` API instead.

- **`tools/solistest/main.go`** — CLI flags (`-device`, `-slave`, `-register`, `-timeout`), configures a `modbus.NewRTUClientHandler` for 9600 8N1, and reads one input register (function code 0x04, default 35000 — the Solis inverter type identifier) via `client.ReadInputRegisters`.
- **`tools/b23test/main.go`** — same structure, targeting the ABB B23/B24 power meter. Reads one holding register (function code 0x03, default 23340 / 0x5B2C — line frequency, 0.01 Hz/LSB) via `client.ReadHoldingRegisters`. The ABB meter's Modbus interface only supports function codes 3, 6, and 16 (no 0x04 Read Input Registers), unlike the Solis inverter.

These two tools still don't share a package with each other or with `src/` — each is a self-contained ~60-line `main.go`.

### `src/` — the daemon

Module-relative package paths are `solis2mqtt/src/...` (single `go.mod` at the repo root covers both `tools/` and `src/`).

- **`src/cmd/solis2mqtt/main.go`** — entry point. Loads `.env` (via `config.LoadDotEnv`, flag `-env`, default `.env`), loads and validates `config.json` (via `config.Load`, flag `-config`, default `config/config.json`), connects to MQTT, builds the serial bus manager, then runs `poller.New(cfg, buses, publisher).Run(ctx)` until SIGINT/SIGTERM.
- **`src/internal/config`** — parses and validates `config.json` (`Config.Load`) and `.env`-style files (`LoadDotEnv`, which sets process env vars without overriding ones already set — so real env vars from e.g. docker-compose always win). `Config` has four top-level sections: `timing` (`pollingInterval`/`interReadDelay`, both ms), `links` (serial ports, currently only `linkType: "modbusRTU"`), `devices` (a link + slave address + register table + MQTT topic), and `registerTables` (named tables of `registerClusters`, each a list of `RegisterDef`: address, Modbus function code 3 or 4, size, data type, scale factor, output property name, optional `outputDecimals` — a `*int` so an explicit `0` is distinguishable from "not set"). `Load` validates cross-references (device→link, device→table), per-register consistency (declared `modbusSize` must match `dataType`'s expected size, `outputDecimals` if set must be >= 0), and per-cluster layout (`validateClusterLayout`: every register in a cluster must share one function code, and sorted by address must form a gap-free, non-overlapping, ascending block) up front, so bad config fails fast at startup rather than mid-poll.
- **`src/internal/serialbus`** — one `Bus` per configured link, shared by every device on that link (Modbus RTU is half-duplex/single-conversation, so each `Bus` is mutex-guarded). Connections are opened lazily on first use; on a failed read the underlying handler is closed so the *next* call reconnects from scratch — a disconnected or misbehaving serial port doesn't require restarting the daemon.
- **`src/internal/registers`** — pure register math, no I/O. `Span(cluster)` computes the single contiguous Modbus read (function code, start address, quantity) that covers every register in a cluster (all registers in a cluster must share one function code, since they're fetched in one request). `Decode(block, blockStart, def)` slices the right bytes out of that block, decodes+scales it per `dataType` (`uint16`, `int16`, `uint32`, `int32`, `float32`; big-endian; `scaleFactor: 0` means ×1), and rounds the result to `OutputDecimals(def)` decimal places (`def.OutputDecimals` if set, else `DefaultOutputDecimals` = 2). Note the returned `float64` can't itself carry trailing zeros (23.8 and 23.80 are the same float64) — formatting to a fixed number of decimals for output is the caller's job.
- **`src/internal/poller`** — the main loop. For each device: for each register cluster in its table, compute the span, read it via the device's `Bus`, decode every register in the cluster, format it with `strconv.FormatFloat(v, 'f', registers.OutputDecimals(r), 64)` (this is what preserves e.g. a trailing `23.80` instead of `encoding/json`'s default shortest-round-trip `23.8`), and store it as `json.RawMessage` in a `map[string]json.RawMessage` keyed by `outputProperty` — `json.Marshal` on that map copies each value's bytes verbatim. After an optional `interReadDelay` between clusters, the merged map is marshaled to JSON and published to the device's `mqttTopic`. A cluster or device read failure is logged and skipped, not fatal — one bad device/cluster doesn't stop the round. Repeats every `pollingInterval` until the context is cancelled.
- **`src/internal/mqttpub`** — thin wrapper around `github.com/eclipse/paho.mqtt.golang`. `EnvConfigFromEnv()` reads `MQTT_HOST` (required), `MQTT_PORT` (default 1883), `MQTT_USERNAME`/`MQTT_PASSWORD` (optional), `MQTT_CLIENT_ID` (default `solis2mqtt`). `Connect` enables `AutoReconnect` and connect-retry-with-backoff, so a broker that isn't up yet at startup, or a later network blip, doesn't require restarting the daemon. `Publish` sends QoS 1, retained; if the client is currently disconnected it logs and no-ops rather than erroring, since auto-reconnect will restore the connection before the next polling round.
- **`src/config/config.json`** and **`src/.env.example`** — the actual runtime config and an example env file; `src/.env` (real credentials) is gitignored. `doc/config_template.json` is the schema reference, not runtime config.
- **`src/Dockerfile`** / **`src/docker-compose.yml`** — multi-stage build (`golang:1.24` → `alpine:3.20`); the Docker build context is the *repo root* (`context: ..` in the compose file), not `src/`, because the Go module files live at the root. Compose mounts `./config` read-only into the container and passes `/dev/ttyS0` through as a device.

## Non-obvious protocol/design details worth preserving

- **Register addressing has no offset.** The decimal register addresses in the vendor PDFs map directly to the address passed to `ReadInputRegisters`/`ReadHoldingRegisters`/`RegisterDef.RegisterAddress` (e.g. Solis register 33001 → address `33001`, wire bytes `80 E9`). Don't apply the usual Modbus `40001`-style offset conventions here. This holds for both the Solis and ABB register maps, and for `config.json` register entries.
- **Clusters exist to batch reads.** Registers are grouped into `registerClusters` specifically so contiguous registers can be fetched with one Modbus request (`registers.Span`); all registers in a cluster must share the same function code (3 or 4) or config validation/`Span` will reject it.
- **Bad config fails fast; bad reads don't.** `config.Load` validates the whole file at startup (unknown references, size/dataType mismatches, etc.) and refuses to start on error. Once running, a failed cluster read, decode error, or publish error is logged and the poller moves on to the next cluster/device/round — it does not crash the daemon.
