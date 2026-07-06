package mqttpub_test

import (
	"testing"

	"solis2mqtt/src/internal/mqttpub"
)

func TestEnvConfigFromEnv_MissingHost(t *testing.T) {
	t.Setenv("MQTT_HOST", "")
	if _, err := mqttpub.EnvConfigFromEnv(); err == nil {
		t.Fatal("expected an error when MQTT_HOST is unset")
	}
}

func TestEnvConfigFromEnv_Defaults(t *testing.T) {
	t.Setenv("MQTT_HOST", "broker.local")
	t.Setenv("MQTT_PORT", "")
	t.Setenv("MQTT_CLIENT_ID", "")
	t.Setenv("MQTT_USERNAME", "")
	t.Setenv("MQTT_PASSWORD", "")

	cfg, err := mqttpub.EnvConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	want := mqttpub.EnvConfig{Host: "broker.local", Port: "1883", ClientID: "solis2mqtt"}
	if cfg != want {
		t.Errorf("got %+v, want %+v", cfg, want)
	}
}

func TestEnvConfigFromEnv_ExplicitValues(t *testing.T) {
	t.Setenv("MQTT_HOST", "broker.local")
	t.Setenv("MQTT_PORT", "8883")
	t.Setenv("MQTT_CLIENT_ID", "custom")
	t.Setenv("MQTT_USERNAME", "alice")
	t.Setenv("MQTT_PASSWORD", "secret")

	cfg, err := mqttpub.EnvConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	want := mqttpub.EnvConfig{Host: "broker.local", Port: "8883", Username: "alice", Password: "secret", ClientID: "custom"}
	if cfg != want {
		t.Errorf("got %+v, want %+v", cfg, want)
	}
}
