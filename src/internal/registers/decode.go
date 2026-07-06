// Package registers decodes raw Modbus register bytes into scaled numeric
// values, and works out how to cover a whole register cluster with a single
// Modbus read.
package registers

import (
	"encoding/binary"
	"fmt"
	"math"

	"solis2mqtt/src/internal/config"
)

// ClusterSpan describes the single Modbus read needed to cover every
// register in a cluster.
type ClusterSpan struct {
	FunctionCode int    // 3 = ReadHoldingRegisters, 4 = ReadInputRegisters
	StartAddress uint16 // lowest registerAddress in the cluster
	Quantity     uint16 // number of 16-bit registers to read, starting at StartAddress
}

// Span computes the ClusterSpan covering every register in cl. All
// registers in a cluster must share the same modbusReadCommand, since they
// are fetched with a single Modbus request.
func Span(cl config.RegisterCluster) (ClusterSpan, error) {
	if len(cl.Registers) == 0 {
		return ClusterSpan{}, fmt.Errorf("cluster %s has no registers", cl.ClusterName)
	}

	fc := cl.Registers[0].ModbusReadCommand
	start := cl.Registers[0].RegisterAddress
	end := cl.Registers[0].RegisterAddress + cl.Registers[0].ModbusSize

	for _, r := range cl.Registers[1:] {
		if r.ModbusReadCommand != fc {
			return ClusterSpan{}, fmt.Errorf("cluster %s: mixed modbusReadCommand (%d and %d) not supported in a single read", cl.ClusterName, fc, r.ModbusReadCommand)
		}
		if r.RegisterAddress < start {
			start = r.RegisterAddress
		}
		if e := r.RegisterAddress + r.ModbusSize; e > end {
			end = e
		}
	}

	return ClusterSpan{
		FunctionCode: fc,
		StartAddress: start,
		Quantity:     end - start,
	}, nil
}

// Decode extracts def's value out of block, a register block that was read
// starting at blockStart (as returned by Span), and applies def.ScaleFactor.
func Decode(block []byte, blockStart uint16, def config.RegisterDef) (float64, error) {
	offset := int(def.RegisterAddress-blockStart) * 2
	length := int(def.ModbusSize) * 2
	if offset < 0 || offset+length > len(block) {
		return 0, fmt.Errorf("register %d: out of range of read block (offset %d, len %d, block size %d)", def.RegisterAddress, offset, length, len(block))
	}
	raw := block[offset : offset+length]

	factor := def.ScaleFactor
	if factor == 0 {
		factor = 1
	}

	switch def.DataType {
	case "uint16":
		return float64(binary.BigEndian.Uint16(raw)) * factor, nil
	case "int16":
		return float64(int16(binary.BigEndian.Uint16(raw))) * factor, nil
	case "uint32":
		return float64(binary.BigEndian.Uint32(raw)) * factor, nil
	case "int32":
		return float64(int32(binary.BigEndian.Uint32(raw))) * factor, nil
	case "float32":
		return float64(math.Float32frombits(binary.BigEndian.Uint32(raw))) * factor, nil
	default:
		return 0, fmt.Errorf("register %d: unsupported dataType %q", def.RegisterAddress, def.DataType)
	}
}
