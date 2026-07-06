// Command solis2mqtt reads Modbus RTU registers from the devices in
// config.json and publishes them as JSON to an MQTT broker, on a repeating
// schedule.
package main

import (
	"context"
	"flag"
	"log"
	"os/signal"
	"syscall"
	"time"

	"solis2mqtt/src/internal/config"
	"solis2mqtt/src/internal/mqttpub"
	"solis2mqtt/src/internal/poller"
	"solis2mqtt/src/internal/serialbus"
)

const readTimeout = 3 * time.Second

func main() {
	configPath := flag.String("config", "config/config.json", "path to config.json")
	envPath := flag.String("env", ".env", "path to .env file with MQTT broker credentials")
	flag.Parse()

	if err := config.LoadDotEnv(*envPath); err != nil {
		log.Fatalf("load %s: %v", *envPath, err)
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	mqttEnv, err := mqttpub.EnvConfigFromEnv()
	if err != nil {
		log.Fatalf("mqtt config: %v", err)
	}

	publisher, err := mqttpub.Connect(mqttEnv)
	if err != nil {
		log.Fatalf("mqtt connect: %v", err)
	}
	defer publisher.Disconnect()

	buses := serialbus.NewManager(cfg.Links, readTimeout)
	defer buses.CloseAll()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Printf("solis2mqtt: polling %d device(s) every %dms", len(cfg.Devices), cfg.Timing.PollingIntervalMS)
	poller.New(cfg, buses, publisher).Run(ctx)

	log.Println("solis2mqtt: shutting down")
}
