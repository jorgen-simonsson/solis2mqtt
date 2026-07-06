// Package mqttpub wraps the Eclipse Paho MQTT client with the settings this
// project needs: broker credentials from the environment, and automatic
// reconnection so a broker restart or network blip doesn't require
// restarting solis2mqtt.
package mqttpub

import (
	"fmt"
	"log"
	"os"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// Publisher publishes JSON payloads to an MQTT broker.
type Publisher struct {
	client mqtt.Client
}

// EnvConfig is the broker connection info read from the process
// environment (normally populated via .env): MQTT_HOST, MQTT_PORT,
// MQTT_USERNAME, MQTT_PASSWORD, MQTT_CLIENT_ID.
type EnvConfig struct {
	Host     string
	Port     string
	Username string
	Password string
	ClientID string
}

// EnvConfigFromEnv reads EnvConfig from the process environment.
func EnvConfigFromEnv() (EnvConfig, error) {
	host := os.Getenv("MQTT_HOST")
	if host == "" {
		return EnvConfig{}, fmt.Errorf("MQTT_HOST is not set")
	}
	port := os.Getenv("MQTT_PORT")
	if port == "" {
		port = "1883"
	}
	clientID := os.Getenv("MQTT_CLIENT_ID")
	if clientID == "" {
		clientID = "solis2mqtt"
	}
	return EnvConfig{
		Host:     host,
		Port:     port,
		Username: os.Getenv("MQTT_USERNAME"),
		Password: os.Getenv("MQTT_PASSWORD"),
		ClientID: clientID,
	}, nil
}

// Connect dials the broker described by cfg. AutoReconnect is enabled so
// the client keeps retrying in the background on connection loss; Connect
// itself also retries with backoff until ctx-independent success, since the
// broker may not be up yet when solis2mqtt starts.
func Connect(cfg EnvConfig) (*Publisher, error) {
	opts := mqtt.NewClientOptions()
	opts.AddBroker(fmt.Sprintf("tcp://%s:%s", cfg.Host, cfg.Port))
	opts.SetClientID(cfg.ClientID)
	if cfg.Username != "" {
		opts.SetUsername(cfg.Username)
	}
	if cfg.Password != "" {
		opts.SetPassword(cfg.Password)
	}
	opts.SetAutoReconnect(true)
	opts.SetMaxReconnectInterval(1 * time.Minute)
	opts.SetConnectRetry(true)
	opts.SetConnectRetryInterval(5 * time.Second)
	opts.SetConnectionLostHandler(func(_ mqtt.Client, err error) {
		log.Printf("mqtt: connection lost: %v", err)
	})
	opts.SetOnConnectHandler(func(_ mqtt.Client) {
		log.Printf("mqtt: connected to %s:%s", cfg.Host, cfg.Port)
	})
	opts.SetReconnectingHandler(func(_ mqtt.Client, _ *mqtt.ClientOptions) {
		log.Printf("mqtt: reconnecting to %s:%s", cfg.Host, cfg.Port)
	})

	client := mqtt.NewClient(opts)
	token := client.Connect()
	token.Wait()
	if err := token.Error(); err != nil {
		return nil, fmt.Errorf("connect to mqtt broker %s:%s: %w", cfg.Host, cfg.Port, err)
	}

	return &Publisher{client: client}, nil
}

// Publish sends payload to topic with QoS 1, retained. If the client is
// currently disconnected the call is skipped (logged, not an error) since
// AutoReconnect will restore the connection in the background before the
// next polling round.
func (p *Publisher) Publish(topic string, payload []byte) error {
	if !p.client.IsConnected() {
		log.Printf("mqtt: not connected, dropping publish to %s", topic)
		return nil
	}

	token := p.client.Publish(topic, 1, true, payload)
	token.Wait()
	return token.Error()
}

// Disconnect closes the connection to the broker.
func (p *Publisher) Disconnect() {
	p.client.Disconnect(250)
}
