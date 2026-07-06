// Package poller implements the main polling loop: for every configured
// device, read each register cluster with one Modbus request, merge the
// decoded values into a single JSON payload, and publish it to the
// device's MQTT topic.
package poller

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"time"

	"solis2mqtt/src/internal/config"
	"solis2mqtt/src/internal/mqttpub"
	"solis2mqtt/src/internal/registers"
	"solis2mqtt/src/internal/serialbus"
)

// Poller repeatedly reads every configured device and publishes its values.
type Poller struct {
	cfg       *config.Config
	buses     *serialbus.Manager
	publisher *mqttpub.Publisher
}

// New builds a Poller from a loaded config, its serial bus manager and an
// already-connected MQTT publisher.
func New(cfg *config.Config, buses *serialbus.Manager, publisher *mqttpub.Publisher) *Poller {
	return &Poller{cfg: cfg, buses: buses, publisher: publisher}
}

// Run polls every device, waits the configured polling interval, and
// repeats until ctx is cancelled.
func (p *Poller) Run(ctx context.Context) {
	interval := time.Duration(p.cfg.Timing.PollingIntervalMS) * time.Millisecond

	for {
		p.pollAll(ctx)

		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}

func (p *Poller) pollAll(ctx context.Context) {
	for _, dev := range p.cfg.Devices {
		if ctx.Err() != nil {
			return
		}
		if err := p.pollDevice(ctx, dev); err != nil {
			log.Printf("device %s: %v", dev.DeviceID, err)
		}
	}
}

func (p *Poller) pollDevice(ctx context.Context, dev config.Device) error {
	bus, ok := p.buses.Bus(dev.LinkID)
	if !ok {
		return fmt.Errorf("unknown linkId %q", dev.LinkID)
	}
	table, ok := p.cfg.TableByID(dev.TableID)
	if !ok {
		return fmt.Errorf("unknown tableId %q", dev.TableID)
	}

	interReadDelay := time.Duration(p.cfg.Timing.InterReadDelayMS) * time.Millisecond
	// json.RawMessage rather than float64: encoding/json always prints
	// floats with the shortest representation that round-trips (23.8, never
	// 23.80), so a fixed number of decimal places has to be formatted as a
	// string and inserted verbatim as the raw number token.
	payload := make(map[string]json.RawMessage)

	for i, cl := range table.RegisterClusters {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		span, err := registers.Span(cl)
		if err != nil {
			log.Printf("device %s: cluster %s: %v", dev.DeviceID, cl.ClusterName, err)
			continue
		}

		block, err := bus.Read(ctx, byte(dev.DeviceAddress), span.FunctionCode, span.StartAddress, span.Quantity)
		if err != nil {
			log.Printf("device %s: cluster %s: read failed: %v", dev.DeviceID, cl.ClusterName, err)
			continue
		}

		for _, r := range cl.Registers {
			v, err := registers.Decode(block, span.StartAddress, r)
			if err != nil {
				log.Printf("device %s: cluster %s: %v", dev.DeviceID, cl.ClusterName, err)
				continue
			}
			formatted := strconv.FormatFloat(v, 'f', registers.OutputDecimals(r), 64)
			payload[r.OutputProperty] = json.RawMessage(formatted)
		}

		if i < len(table.RegisterClusters)-1 && interReadDelay > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(interReadDelay):
			}
		}
	}

	if len(payload) == 0 {
		return fmt.Errorf("no register values were read, skipping publish")
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	if err := p.publisher.Publish(dev.MQTTTopic, data); err != nil {
		return fmt.Errorf("publish to %s: %w", dev.MQTTTopic, err)
	}

	return nil
}
