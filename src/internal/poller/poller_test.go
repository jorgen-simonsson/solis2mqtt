//go:build !windows

// This file exercises the full pipeline end to end without any physical
// hardware or external services: a github.com/creack/pty virtual serial
// pair stands in for the RS485 link (with a hand-rolled Modbus RTU slave
// answering on the other end), and an embedded github.com/mochi-mqtt/server
// broker stands in for the MQTT broker. It asserts the exact JSON payload
// that comes out the other side.
package poller

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"testing"
	"time"

	"github.com/creack/pty"
	mqttbroker "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"
	"github.com/mochi-mqtt/server/v2/packets"

	"solis2mqtt/src/internal/config"
	"solis2mqtt/src/internal/mqttpub"
	"solis2mqtt/src/internal/serialbus"
)

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

func TestPoller_PollAll_Integration(t *testing.T) {
	master, slave, err := pty.Open()
	if err != nil {
		t.Skipf("pty not available in this environment: %v", err)
	}
	defer master.Close()
	defer slave.Close()

	// Fake ABB-style slave: answers one ReadHoldingRegisters request for
	// registers 100-101 with temp=1234 (uint16) and current=-5 (int16,
	// scaled by 0.1 -> -0.5).
	go func() {
		req := make([]byte, 8)
		if _, err := io.ReadFull(master, req); err != nil {
			return
		}
		resp := []byte{1, 3, 4, 0x04, 0xD2, 0xFF, 0xFB}
		crc := modbusCRC(resp)
		resp = append(resp, byte(crc), byte(crc>>8))
		master.Write(resp)
	}()

	cfg := &config.Config{
		Timing: config.Timing{PollingIntervalMS: 1000, InterReadDelayMS: 0},
		Links: []config.Link{
			{LinkID: "link1", LinkType: "modbusRTU", LinkName: slave.Name(), BaudRate: 9600, Parity: "none", DataBits: 8, StopBits: 1},
		},
		Devices: []config.Device{
			{LinkID: "link1", DeviceID: "dev1", DeviceAddress: 1, TableID: "table1", MQTTTopic: "modbus/dev1"},
		},
		RegisterTables: []config.RegisterTable{
			{
				TableID: "table1",
				RegisterClusters: []config.RegisterCluster{
					{
						ClusterName: "cluster1",
						Registers: []config.RegisterDef{
							{RegisterAddress: 100, ModbusReadCommand: 3, ModbusSize: 1, ScaleFactor: 1, DataType: "uint16", OutputProperty: "temp"},
							{RegisterAddress: 101, ModbusReadCommand: 3, ModbusSize: 1, ScaleFactor: 0.1, DataType: "int16", OutputProperty: "current"},
						},
					},
				},
			},
		},
	}

	buses := serialbus.NewManager(cfg.Links, 2*time.Second)
	defer buses.CloseAll()

	srv := mqttbroker.New(&mqttbroker.Options{InlineClient: true})
	if err := srv.AddHook(new(auth.AllowHook), nil); err != nil {
		t.Fatalf("add hook: %v", err)
	}
	tcp := listeners.NewTCP(listeners.Config{ID: "test", Address: "127.0.0.1:0"})
	if err := srv.AddListener(tcp); err != nil {
		t.Fatalf("add listener: %v", err)
	}

	received := make(chan []byte, 4)
	if err := srv.Subscribe("modbus/#", 1, func(_ *mqttbroker.Client, _ packets.Subscription, pk packets.Packet) {
		select {
		case received <- pk.Payload:
		default:
		}
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	go func() { _ = srv.Serve() }()
	defer srv.Close()

	host, port, err := net.SplitHostPort(tcp.Address())
	if err != nil {
		t.Fatalf("split broker address: %v", err)
	}

	publisher, err := mqttpub.Connect(mqttpub.EnvConfig{Host: host, Port: port, ClientID: "poller-test"})
	if err != nil {
		t.Fatalf("mqtt connect: %v", err)
	}
	defer publisher.Disconnect()

	New(cfg, buses, publisher).pollAll(context.Background())

	select {
	case payload := <-received:
		// Assert the exact bytes, not just the parsed numeric value: both
		// registers default to 2 output decimals, and json.Unmarshal into a
		// float64 can't distinguish "1234.00" from "1234" or "1234.0" (they're
		// the same float64), which would hide a regression in the fixed
		// decimal-place formatting.
		wantJSON := `{"current":-0.50,"temp":1234.00}`
		if string(payload) != wantJSON {
			t.Errorf("payload = %s, want %s", payload, wantJSON)
		}

		var got map[string]float64
		if err := json.Unmarshal(payload, &got); err != nil {
			t.Fatalf("unmarshal payload %s: %v", payload, err)
		}
		want := map[string]float64{"temp": 1234, "current": -0.5}
		if len(got) != len(want) {
			t.Fatalf("got %v, want %v", got, want)
		}
		for k, v := range want {
			if got[k] != v {
				t.Errorf("payload[%q] = %v, want %v", k, got[k], v)
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the published payload")
	}
}
