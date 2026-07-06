// This file exercises Publisher against a real MQTT broker without any
// external services: github.com/mochi-mqtt/server/v2 runs an in-process,
// pure-Go broker on a loopback port for the duration of each test.
package mqttpub_test

import (
	"net"
	"testing"
	"time"

	mqttbroker "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"
	"github.com/mochi-mqtt/server/v2/packets"

	"solis2mqtt/src/internal/mqttpub"
)

// startBroker starts an embedded MQTT broker on a loopback port, allowing
// all connections and subscribing itself to every topic so tests can
// observe published payloads on the returned channel. It is torn down
// automatically at the end of the test.
func startBroker(t *testing.T) (host, port string, received chan []byte) {
	t.Helper()

	srv := mqttbroker.New(&mqttbroker.Options{InlineClient: true})
	if err := srv.AddHook(new(auth.AllowHook), nil); err != nil {
		t.Fatalf("add hook: %v", err)
	}

	tcp := listeners.NewTCP(listeners.Config{ID: "test", Address: "127.0.0.1:0"})
	if err := srv.AddListener(tcp); err != nil {
		t.Fatalf("add listener: %v", err)
	}

	// Scoped to "modbus/#" (our own topic namespace) rather than "#", so we
	// don't also pick up the broker's periodic $SYS stats. The handler
	// sends non-blockingly so a test that doesn't drain the channel can
	// never wedge the broker's internal publish goroutine.
	received = make(chan []byte, 16)
	if err := srv.Subscribe("modbus/#", 1, func(_ *mqttbroker.Client, _ packets.Subscription, pk packets.Packet) {
		select {
		case received <- pk.Payload:
		default:
		}
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	go func() {
		_ = srv.Serve()
	}()
	t.Cleanup(func() { srv.Close() })

	host, port, err := net.SplitHostPort(tcp.Address())
	if err != nil {
		t.Fatalf("split broker address %q: %v", tcp.Address(), err)
	}
	return host, port, received
}

func TestPublisher_PublishRoundTrip(t *testing.T) {
	host, port, received := startBroker(t)

	pub, err := mqttpub.Connect(mqttpub.EnvConfig{Host: host, Port: port, ClientID: "test-publisher"})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pub.Disconnect()

	payload := []byte(`{"voltageL1":230.5}`)
	if err := pub.Publish("modbus/ABB_B23", payload); err != nil {
		t.Fatalf("publish: %v", err)
	}

	select {
	case got := <-received:
		if string(got) != string(payload) {
			t.Errorf("got payload %s, want %s", got, payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the published message")
	}
}

func TestPublisher_PublishAfterDisconnectIsNoop(t *testing.T) {
	host, port, _ := startBroker(t)

	pub, err := mqttpub.Connect(mqttpub.EnvConfig{Host: host, Port: port, ClientID: "test-disconnected"})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	pub.Disconnect()

	if err := pub.Publish("modbus/whatever", []byte("{}")); err != nil {
		t.Fatalf("Publish while disconnected should be a silent no-op, got error: %v", err)
	}
}
