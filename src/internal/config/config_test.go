package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeConfig(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const validConfigJSON = `{
  "timing": {"pollingInterval": 1000, "interReadDelay": 50},
  "links": [{"linkId":"link1","linkType":"modbusRTU","linkName":"/dev/ttyS0","baudrate":9600,"parity":"none","dataBits":8,"stopBits":1}],
  "devices": [{"linkId":"link1","deviceId":"dev1","deviceAddress":1,"tableId":"table1","mqttTopic":"modbus/dev1"}],
  "registerTables": [{
    "tableId": "table1",
    "registerClusters": [{
      "clusterName": "cluster1",
      "registers": [
        {"registerAddress":100,"modbusReadCommand":3,"modbusSize":1,"scaleFactor":1,"dataType":"uint16","outputProperty":"temp"},
        {"registerAddress":101,"modbusReadCommand":3,"modbusSize":2,"scaleFactor":0.1,"dataType":"float32","outputProperty":"voltage"}
      ]
    }]
  }]
}`

func TestLoad_Valid(t *testing.T) {
	cfg, err := Load(writeConfig(t, validConfigJSON))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Timing.PollingIntervalMS != 1000 {
		t.Errorf("PollingIntervalMS = %d, want 1000", cfg.Timing.PollingIntervalMS)
	}
	if cfg.Timing.InterReadDelayMS != 50 {
		t.Errorf("InterReadDelayMS = %d, want 50", cfg.Timing.InterReadDelayMS)
	}
	if len(cfg.Devices) != 1 {
		t.Fatalf("len(Devices) = %d, want 1", len(cfg.Devices))
	}

	table, ok := cfg.TableByID("table1")
	if !ok {
		t.Fatal("expected table1 to be found")
	}
	if len(table.RegisterClusters) != 1 || len(table.RegisterClusters[0].Registers) != 2 {
		t.Fatalf("unexpected table shape: %+v", table)
	}

	link, ok := cfg.LinkByID("link1")
	if !ok {
		t.Fatal("expected link1 to be found")
	}
	if link.LinkName != "/dev/ttyS0" {
		t.Errorf("LinkName = %q, want /dev/ttyS0", link.LinkName)
	}

	if _, ok := cfg.TableByID("nope"); ok {
		t.Error("expected unknown tableId to be not found")
	}
	if _, ok := cfg.LinkByID("nope"); ok {
		t.Error("expected unknown linkId to be not found")
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("expected an error for a missing config file")
	}
}

func TestLoad_InvalidJSON(t *testing.T) {
	if _, err := Load(writeConfig(t, "{not valid json")); err == nil {
		t.Fatal("expected an error for malformed JSON")
	}
}

// validConfig returns a fresh, valid Config for mutation in table-driven
// validation tests. Each call builds new slices so tests can't bleed into
// each other.
func validConfig() Config {
	return Config{
		Timing: Timing{PollingIntervalMS: 1000, InterReadDelayMS: 50},
		Links: []Link{
			{LinkID: "link1", LinkType: "modbusRTU", LinkName: "/dev/ttyS0", BaudRate: 9600, Parity: "none", DataBits: 8, StopBits: 1},
		},
		Devices: []Device{
			{LinkID: "link1", DeviceID: "dev1", DeviceAddress: 1, TableID: "table1", MQTTTopic: "modbus/dev1"},
		},
		RegisterTables: []RegisterTable{
			{
				TableID: "table1",
				RegisterClusters: []RegisterCluster{
					{
						ClusterName: "cluster1",
						Registers: []RegisterDef{
							{RegisterAddress: 100, ModbusReadCommand: 3, ModbusSize: 1, ScaleFactor: 1, DataType: "uint16", OutputProperty: "temp"},
						},
					},
				},
			},
		},
	}
}

func TestValidate_Valid(t *testing.T) {
	cfg := validConfig()
	if err := cfg.validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_Errors(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{"zero polling interval", func(c *Config) { c.Timing.PollingIntervalMS = 0 }},
		{"negative inter-read delay", func(c *Config) { c.Timing.InterReadDelayMS = -1 }},
		{"empty link id", func(c *Config) { c.Links[0].LinkID = "" }},
		{"unsupported link type", func(c *Config) { c.Links[0].LinkType = "modbusTCP" }},
		{"empty link name", func(c *Config) { c.Links[0].LinkName = "" }},
		{"duplicate link id", func(c *Config) { c.Links = append(c.Links, c.Links[0]) }},
		{"empty table id", func(c *Config) { c.RegisterTables[0].TableID = "" }},
		{"duplicate table id", func(c *Config) { c.RegisterTables = append(c.RegisterTables, c.RegisterTables[0]) }},
		{"cluster with no registers", func(c *Config) { c.RegisterTables[0].RegisterClusters[0].Registers = nil }},
		{"zero modbus size", func(c *Config) { c.RegisterTables[0].RegisterClusters[0].Registers[0].ModbusSize = 0 }},
		{"unsupported read command", func(c *Config) { c.RegisterTables[0].RegisterClusters[0].Registers[0].ModbusReadCommand = 5 }},
		{"empty output property", func(c *Config) { c.RegisterTables[0].RegisterClusters[0].Registers[0].OutputProperty = "" }},
		{"unknown data type", func(c *Config) { c.RegisterTables[0].RegisterClusters[0].Registers[0].DataType = "bogus" }},
		{"modbus size wrong for data type", func(c *Config) {
			c.RegisterTables[0].RegisterClusters[0].Registers[0].DataType = "uint32" // wants size 2, still 1
		}},
		{"no devices", func(c *Config) { c.Devices = nil }},
		{"empty device id", func(c *Config) { c.Devices[0].DeviceID = "" }},
		{"duplicate device id", func(c *Config) { c.Devices = append(c.Devices, c.Devices[0]) }},
		{"device references unknown link", func(c *Config) { c.Devices[0].LinkID = "nope" }},
		{"device references unknown table", func(c *Config) { c.Devices[0].TableID = "nope" }},
		{"device address zero", func(c *Config) { c.Devices[0].DeviceAddress = 0 }},
		{"device address too large", func(c *Config) { c.Devices[0].DeviceAddress = 256 }},
		{"empty mqtt topic", func(c *Config) { c.Devices[0].MQTTTopic = "" }},
		{"cluster registers overlap", func(c *Config) {
			regs := &c.RegisterTables[0].RegisterClusters[0].Registers
			// first register is address 100, size 1 (covers just 100); this
			// one starts at 100 too, so it overlaps rather than following on.
			*regs = append(*regs, RegisterDef{RegisterAddress: 100, ModbusReadCommand: 3, ModbusSize: 1, DataType: "uint16", OutputProperty: "other"})
		}},
		{"cluster registers have a gap", func(c *Config) {
			regs := &c.RegisterTables[0].RegisterClusters[0].Registers
			// first register ends at 101 (100 + size 1); this one starts at
			// 102, leaving register 101 unaccounted for.
			*regs = append(*regs, RegisterDef{RegisterAddress: 102, ModbusReadCommand: 3, ModbusSize: 1, DataType: "uint16", OutputProperty: "other"})
		}},
		{"cluster mixes modbus read commands", func(c *Config) {
			regs := &c.RegisterTables[0].RegisterClusters[0].Registers
			*regs = append(*regs, RegisterDef{RegisterAddress: 101, ModbusReadCommand: 4, ModbusSize: 1, DataType: "uint16", OutputProperty: "other"})
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.mutate(&cfg)
			if err := cfg.validate(); err == nil {
				t.Fatal("expected an error, got nil")
			}
		})
	}
}
