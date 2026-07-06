// Package config loads the application configuration (config.json) and the
// broker credentials (.env / process environment) that together describe
// which serial links, devices and registers solis2mqtt should poll, and
// which MQTT broker it should publish to.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

// Config is the root of config.json, see doc/config_template.json.
type Config struct {
	Timing         Timing          `json:"timing"`
	Links          []Link          `json:"links"`
	Devices        []Device        `json:"devices"`
	RegisterTables []RegisterTable `json:"registerTables"`
}

type Timing struct {
	// PollingIntervalMS is how long to wait after a full round of devices
	// has been read before starting the next round.
	PollingIntervalMS int `json:"pollingInterval"`
	// InterReadDelayMS is how long to wait between two consecutive Modbus
	// reads (i.e. between register clusters).
	InterReadDelayMS int `json:"interReadDelay"`
}

type Link struct {
	LinkID   string `json:"linkId"`
	LinkType string `json:"linkType"` // only "modbusRTU" is currently supported
	LinkName string `json:"linkName"` // serial device path, e.g. /dev/ttyS0
	BaudRate int    `json:"baudrate"`
	Parity   string `json:"parity"` // "none", "even" or "odd"
	DataBits int    `json:"dataBits"`
	StopBits int    `json:"stopBits"`
}

type Device struct {
	LinkID        string `json:"linkId"`
	DeviceID      string `json:"deviceId"`
	DeviceAddress int    `json:"deviceAddress"` // Modbus slave address
	TableID       string `json:"tableId"`
	MQTTTopic     string `json:"mqttTopic"`
}

type RegisterTable struct {
	TableID          string            `json:"tableId"`
	RegisterClusters []RegisterCluster `json:"registerClusters"`
}

type RegisterCluster struct {
	ClusterName string        `json:"clusterName"`
	Registers   []RegisterDef `json:"registers"`
}

type RegisterDef struct {
	RegisterAddress   uint16  `json:"registerAddress"`
	ModbusReadCommand int     `json:"modbusReadCommand"` // 3 = ReadHoldingRegisters, 4 = ReadInputRegisters
	ModbusSize        uint16  `json:"modbusSize"`        // number of 16-bit registers
	ScaleFactor       float64 `json:"scaleFactor"`
	DataType          string  `json:"dataType"` // uint16, int16, uint32, int32, float32
	OutputProperty    string  `json:"outputProperty"`
	// OutputDecimals overrides the number of decimal places the published
	// value is rounded to (registers.DefaultOutputDecimals if nil). A
	// pointer so an explicit 0 (round to a whole number) is distinguishable
	// from "not set".
	OutputDecimals *int `json:"outputDecimals,omitempty"`
}

// dataTypeSizes maps each supported dataType to the number of 16-bit
// registers it occupies.
var dataTypeSizes = map[string]uint16{
	"uint16":  1,
	"int16":   1,
	"uint32":  2,
	"int32":   2,
	"float32": 2,
}

// Load reads and validates config.json from path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid config %s: %w", path, err)
	}

	return &cfg, nil
}

func (c *Config) validate() error {
	if c.Timing.PollingIntervalMS <= 0 {
		return fmt.Errorf("timing.pollingInterval must be > 0")
	}
	if c.Timing.InterReadDelayMS < 0 {
		return fmt.Errorf("timing.interReadDelay must be >= 0")
	}

	links := make(map[string]Link, len(c.Links))
	for _, l := range c.Links {
		if l.LinkID == "" {
			return fmt.Errorf("link with empty linkId")
		}
		if l.LinkType != "modbusRTU" {
			return fmt.Errorf("link %s: unsupported linkType %q", l.LinkID, l.LinkType)
		}
		if l.LinkName == "" {
			return fmt.Errorf("link %s: linkName is required", l.LinkID)
		}
		if _, exists := links[l.LinkID]; exists {
			return fmt.Errorf("duplicate linkId %q", l.LinkID)
		}
		links[l.LinkID] = l
	}

	tables := make(map[string]RegisterTable, len(c.RegisterTables))
	for _, t := range c.RegisterTables {
		if t.TableID == "" {
			return fmt.Errorf("register table with empty tableId")
		}
		if _, exists := tables[t.TableID]; exists {
			return fmt.Errorf("duplicate tableId %q", t.TableID)
		}
		for _, cl := range t.RegisterClusters {
			if len(cl.Registers) == 0 {
				return fmt.Errorf("table %s: cluster %s has no registers", t.TableID, cl.ClusterName)
			}
			for _, r := range cl.Registers {
				if r.ModbusSize == 0 {
					return fmt.Errorf("table %s: cluster %s: register %d: modbusSize must be > 0", t.TableID, cl.ClusterName, r.RegisterAddress)
				}
				if r.ModbusReadCommand != 3 && r.ModbusReadCommand != 4 {
					return fmt.Errorf("table %s: cluster %s: register %d: unsupported modbusReadCommand %d (must be 3 or 4)", t.TableID, cl.ClusterName, r.RegisterAddress, r.ModbusReadCommand)
				}
				if r.OutputProperty == "" {
					return fmt.Errorf("table %s: cluster %s: register %d: outputProperty is required", t.TableID, cl.ClusterName, r.RegisterAddress)
				}
				wantSize, ok := dataTypeSizes[r.DataType]
				if !ok {
					return fmt.Errorf("table %s: cluster %s: register %d: unsupported dataType %q", t.TableID, cl.ClusterName, r.RegisterAddress, r.DataType)
				}
				if r.ModbusSize != wantSize {
					return fmt.Errorf("table %s: cluster %s: register %d: dataType %q requires modbusSize %d, got %d", t.TableID, cl.ClusterName, r.RegisterAddress, r.DataType, wantSize, r.ModbusSize)
				}
				if r.OutputDecimals != nil && *r.OutputDecimals < 0 {
					return fmt.Errorf("table %s: cluster %s: register %d: outputDecimals must be >= 0", t.TableID, cl.ClusterName, r.RegisterAddress)
				}
			}
			if err := validateClusterLayout(t.TableID, cl); err != nil {
				return err
			}
		}
		tables[t.TableID] = t
	}

	if len(c.Devices) == 0 {
		return fmt.Errorf("no devices configured")
	}
	seenDevice := make(map[string]bool, len(c.Devices))
	for _, d := range c.Devices {
		if d.DeviceID == "" {
			return fmt.Errorf("device with empty deviceId")
		}
		if seenDevice[d.DeviceID] {
			return fmt.Errorf("duplicate deviceId %q", d.DeviceID)
		}
		seenDevice[d.DeviceID] = true
		if _, ok := links[d.LinkID]; !ok {
			return fmt.Errorf("device %s: unknown linkId %q", d.DeviceID, d.LinkID)
		}
		if _, ok := tables[d.TableID]; !ok {
			return fmt.Errorf("device %s: unknown tableId %q", d.DeviceID, d.TableID)
		}
		if d.DeviceAddress <= 0 || d.DeviceAddress > 255 {
			return fmt.Errorf("device %s: deviceAddress must be between 1 and 255", d.DeviceID)
		}
		if d.MQTTTopic == "" {
			return fmt.Errorf("device %s: mqttTopic is required", d.DeviceID)
		}
	}

	return nil
}

// validateClusterLayout checks that cl's registers form one realistic
// Modbus read: every register uses the same function code (registers are
// fetched with a single request per cluster, see registers.Span), and,
// sorted by address, they cover a gap-free, non-overlapping, ascending
// block of registers.
func validateClusterLayout(tableID string, cl RegisterCluster) error {
	regs := make([]RegisterDef, len(cl.Registers))
	copy(regs, cl.Registers)
	sort.Slice(regs, func(i, j int) bool { return regs[i].RegisterAddress < regs[j].RegisterAddress })

	for i := 1; i < len(regs); i++ {
		prev, cur := regs[i-1], regs[i]
		if cur.ModbusReadCommand != prev.ModbusReadCommand {
			return fmt.Errorf("table %s: cluster %s: mixed modbusReadCommand (%d and %d) in one cluster", tableID, cl.ClusterName, prev.ModbusReadCommand, cur.ModbusReadCommand)
		}
		prevEnd := prev.RegisterAddress + prev.ModbusSize
		switch {
		case cur.RegisterAddress < prevEnd:
			return fmt.Errorf("table %s: cluster %s: register %d overlaps register %d (which ends at %d)", tableID, cl.ClusterName, cur.RegisterAddress, prev.RegisterAddress, prevEnd)
		case cur.RegisterAddress > prevEnd:
			return fmt.Errorf("table %s: cluster %s: gap between register %d (ends at %d) and register %d", tableID, cl.ClusterName, prev.RegisterAddress, prevEnd, cur.RegisterAddress)
		}
	}
	return nil
}

// TableByID returns the register table with the given id, if present.
func (c *Config) TableByID(id string) (RegisterTable, bool) {
	for _, t := range c.RegisterTables {
		if t.TableID == id {
			return t, true
		}
	}
	return RegisterTable{}, false
}

// LinkByID returns the link with the given id, if present.
func (c *Config) LinkByID(id string) (Link, bool) {
	for _, l := range c.Links {
		if l.LinkID == id {
			return l, true
		}
	}
	return Link{}, false
}
