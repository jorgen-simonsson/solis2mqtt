# solis2mqtt

Reads data from a Solis hybrid inverter and an ABB B23/B24 power meter over RS485 Modbus RTU, and publishes it as JSON to an MQTT broker on a repeating schedule. Which devices, serial links, registers and MQTT topics to poll is entirely driven by a `config.json` file — adding a new register or device is a config change, not a code change.

## Table of contents

- [Repository layout](#repository-layout)
- [How it works](#how-it-works)
- [Configuration](#configuration)
  - [config.json schema](#configjson-schema)
  - [Environment variables](#environment-variables)
- [Running the service](#running-the-service)
  - [With Docker](#with-docker)
  - [Without Docker](#without-docker)
- [Connectivity-test CLIs](#connectivity-test-clis)
- [Development](#development)
- [Protocol notes](#protocol-notes)

## Repository layout

```
src/
  cmd/solis2mqtt/      daemon entry point (main.go)
  internal/config/     config.json + .env loading and validation
  internal/serialbus/  Modbus RTU serial link management, one Bus per link
  internal/registers/  register-cluster span computation and value decoding
  internal/poller/     the polling loop: read clusters, decode, publish
  internal/mqttpub/    MQTT client wrapper (connect, publish, auto-reconnect)
  config/config.json   the actual runtime config for this deployment
  .env.example         template for broker credentials (copy to .env)
  Dockerfile
  docker-compose.yml
tools/
  solistest/  standalone connectivity-test CLI for the Solis inverter
  b23test/    standalone connectivity-test CLI for the ABB B23/B24 meter
doc/
  RS485_MODBUS(ESINV-33000ID) Hybrid Inverter.pdf  vendor protocol reference (Solis)
  B23_B24_User_Manual.pdf.pdf                       vendor protocol reference (ABB), see ch. 9
  config_template.json                              annotated example config.json
```

## How it works

On startup, the daemon (`src/cmd/solis2mqtt`):

1. Loads `.env` (broker credentials) and `config.json` (links, devices, register tables), validating the whole file up front — an inconsistent config (bad cross-references, mismatched sizes/types, overlapping or gapped register clusters, etc.) causes the process to exit immediately rather than fail partway through a poll.
2. Connects to the MQTT broker, with automatic retry/backoff if it isn't reachable yet.
3. Builds one serial `Bus` per configured link. Connections to the serial port are opened lazily on first use, and any bus that errors on a read is closed and reconnected from scratch on the next attempt — a flaky or temporarily disconnected serial adapter doesn't require restarting the daemon.
4. Repeatedly, forever (until SIGINT/SIGTERM):
   - For every configured device, read each of its register clusters with a single Modbus request per cluster (function code 3 or 4), decode every register in the cluster according to its data type and scale factor, round it to its `outputDecimals` (default 2), and merge the results into one JSON object keyed by `outputProperty`.
   - Publish that JSON object (QoS 1, retained) to the device's configured MQTT topic.
   - A failed cluster read, decode error, or publish error is logged and skipped — it does not crash the daemon or stop the rest of the round.
   - Wait the configured `pollingInterval` and start the next round.

Registers are grouped into clusters specifically so that several contiguous registers can be fetched with one Modbus request instead of one request per register; an optional `interReadDelay` can be inserted between clusters to give the device room to breathe.

## Configuration

### config.json schema

See `doc/config_template.json` for a complete annotated example. The top-level shape:

| Section | Purpose |
|---|---|
| `timing` | `pollingInterval` (ms) — delay between polling rounds. `interReadDelay` (ms) — delay between cluster reads within a device. |
| `links` | Serial ports. Each has a `linkId`, `linkType` (only `"modbusRTU"` today), `linkName` (device path, e.g. `/dev/ttyS0`), `baudrate`, `parity` (`none`/`even`/`odd`), `dataBits`, `stopBits`. |
| `devices` | One entry per physical device: which `linkId` it's on, its Modbus slave `deviceAddress` (1–255), which `tableId` describes its registers, and the `mqttTopic` to publish to. |
| `registerTables` | Named tables of `registerClusters`, each a list of register definitions: `registerAddress`, `modbusReadCommand` (3 = holding, 4 = input registers), `modbusSize` (number of 16-bit words), `dataType` (`uint16`, `int16`, `uint32`, `int32`, `float32`), `scaleFactor` (0 is treated as 1), `outputProperty` (the JSON key the decoded value is published under), and optional `outputDecimals` (decimal places to round the published value to; defaults to 2 if omitted). |

Validation performed at load time (the process refuses to start if any of these fail):

- No duplicate `linkId`, `tableId`, or `deviceId`.
- Every device references a `linkId` and `tableId` that actually exist.
- Every register's `modbusSize` matches what its `dataType` requires, and `modbusReadCommand` is 3 or 4.
- Within a single cluster, every register shares the same `modbusReadCommand`, and — sorted by address — the registers form a gap-free, non-overlapping, ascending block. A cluster is read as one contiguous Modbus request spanning its lowest to highest address, so a "cluster" with gaps, overlaps, or mixed function codes isn't a config the daemon can actually service.
- `outputDecimals`, if set, must be >= 0.

### Environment variables

Broker credentials come from `.env` (see `src/.env.example`) or the real process environment — real environment variables always take precedence over `.env`, so Docker/Compose-injected values win.

| Variable | Required | Default |
|---|---|---|
| `MQTT_HOST` | yes | — |
| `MQTT_PORT` | no | `1883` |
| `MQTT_USERNAME` | no | (none) |
| `MQTT_PASSWORD` | no | (none) |
| `MQTT_CLIENT_ID` | no | `solis2mqtt` |

## Running the service

### With Docker

The `src/` folder contains the daemon, its Dockerfile and Docker Compose file. The build context is the repository root (not `src/`), since the Go module files live there.

```bash
cd src
cp .env.example .env   # fill in MQTT_HOST/MQTT_USERNAME/MQTT_PASSWORD etc.
# edit config/config.json to match your devices, links and register tables
docker compose up --build
```

Compose mounts `./config` read-only into the container and passes `/dev/ttyS0` through as a device — adjust the device path in `docker-compose.yml` if your serial adapter enumerates elsewhere.

### Without Docker

```bash
go run ./src/cmd/solis2mqtt -config src/config/config.json -env src/.env
```

## Connectivity-test CLIs

`tools/solistest` and `tools/b23test` are standalone, single-register connectivity checks predating the daemon — useful for verifying wiring, baud rate, and slave addressing before wiring a device into `config.json`.

```bash
go run ./tools/solistest -device /dev/ttyS0    # reads the Solis inverter type identifier (input register 35000)
go run ./tools/b23test -device /dev/ttyS0      # reads the ABB meter's line frequency (holding register 23340)
```

Both accept `-slave`, `-register`, and `-timeout` flags to target a different register or slave address.

## Development

Requires Go 1.24+.

```bash
go build ./...      # build everything
go vet ./...         # static checks
gofmt -l .           # formatting check (should print nothing)
go test ./...        # run all tests
```

## Protocol notes

- **Register addressing has no offset.** The decimal register addresses in the vendor PDFs map directly to the address used on the wire and in `config.json` (e.g. Solis register 33001 → address `33001`). Don't apply the usual Modbus `40001`-style offset conventions here — this holds for both the Solis and ABB register maps.
- The ABB B23/B24 meter's Modbus interface only supports function codes 3 (read holding registers), 6, and 16 — no function code 4 (read input registers) — unlike the Solis inverter.
- The authoritative references for register addresses, data types, scaling factors, and function codes are the vendor documents in `doc/`. Cross-check any new register support against them rather than guessing.
