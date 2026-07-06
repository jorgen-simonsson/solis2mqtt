package registers

import (
	"encoding/binary"
	"math"
	"testing"

	"solis2mqtt/src/internal/config"
)

func reg(addr, size uint16, fc int) config.RegisterDef {
	return config.RegisterDef{RegisterAddress: addr, ModbusSize: size, ModbusReadCommand: fc}
}

func TestSpan(t *testing.T) {
	t.Run("single register", func(t *testing.T) {
		cl := config.RegisterCluster{ClusterName: "c", Registers: []config.RegisterDef{reg(100, 1, 3)}}
		got, err := Span(cl)
		if err != nil {
			t.Fatal(err)
		}
		want := ClusterSpan{FunctionCode: 3, StartAddress: 100, Quantity: 1}
		if got != want {
			t.Errorf("got %+v, want %+v", got, want)
		}
	})

	t.Run("contiguous multi-register", func(t *testing.T) {
		cl := config.RegisterCluster{ClusterName: "c", Registers: []config.RegisterDef{
			reg(3456, 2, 3), reg(3458, 2, 3),
		}}
		got, err := Span(cl)
		if err != nil {
			t.Fatal(err)
		}
		want := ClusterSpan{FunctionCode: 3, StartAddress: 3456, Quantity: 4}
		if got != want {
			t.Errorf("got %+v, want %+v", got, want)
		}
	})

	t.Run("registers out of address order", func(t *testing.T) {
		cl := config.RegisterCluster{ClusterName: "c", Registers: []config.RegisterDef{
			reg(4, 2, 3), reg(2, 2, 3),
		}}
		got, err := Span(cl)
		if err != nil {
			t.Fatal(err)
		}
		want := ClusterSpan{FunctionCode: 3, StartAddress: 2, Quantity: 4}
		if got != want {
			t.Errorf("got %+v, want %+v", got, want)
		}
	})

	t.Run("mixed function codes rejected", func(t *testing.T) {
		cl := config.RegisterCluster{ClusterName: "c", Registers: []config.RegisterDef{
			reg(100, 1, 3), reg(101, 1, 4),
		}}
		if _, err := Span(cl); err == nil {
			t.Fatal("expected an error for mixed modbusReadCommand values")
		}
	})

	t.Run("empty cluster rejected", func(t *testing.T) {
		if _, err := Span(config.RegisterCluster{ClusterName: "c"}); err == nil {
			t.Fatal("expected an error for an empty cluster")
		}
	})
}

func TestDecode(t *testing.T) {
	block := make([]byte, 8) // registers 100..103, 2 bytes each
	var negFive int16 = -5
	binary.BigEndian.PutUint16(block[0:2], 1234)
	binary.BigEndian.PutUint16(block[2:4], uint16(negFive))
	binary.BigEndian.PutUint32(block[4:8], math.Float32bits(21.5))

	t.Run("uint16", func(t *testing.T) {
		def := config.RegisterDef{RegisterAddress: 100, ModbusSize: 1, ScaleFactor: 1, DataType: "uint16"}
		got, err := Decode(block, 100, def)
		if err != nil {
			t.Fatal(err)
		}
		if got != 1234 {
			t.Errorf("got %v, want 1234", got)
		}
	})

	t.Run("int16 negative with scale factor", func(t *testing.T) {
		def := config.RegisterDef{RegisterAddress: 101, ModbusSize: 1, ScaleFactor: 0.5, DataType: "int16"}
		got, err := Decode(block, 100, def)
		if err != nil {
			t.Fatal(err)
		}
		if got != -2.5 {
			t.Errorf("got %v, want -2.5", got)
		}
	})

	t.Run("float32 with default scale (zero means 1)", func(t *testing.T) {
		def := config.RegisterDef{RegisterAddress: 102, ModbusSize: 2, ScaleFactor: 0, DataType: "float32"}
		got, err := Decode(block, 100, def)
		if err != nil {
			t.Fatal(err)
		}
		if got != 21.5 {
			t.Errorf("got %v, want 21.5", got)
		}
	})

	t.Run("uint32", func(t *testing.T) {
		b := make([]byte, 4)
		binary.BigEndian.PutUint32(b, 70000)
		def := config.RegisterDef{RegisterAddress: 200, ModbusSize: 2, ScaleFactor: 1, DataType: "uint32"}
		got, err := Decode(b, 200, def)
		if err != nil {
			t.Fatal(err)
		}
		if got != 70000 {
			t.Errorf("got %v, want 70000", got)
		}
	})

	t.Run("int32 negative", func(t *testing.T) {
		b := make([]byte, 4)
		var neg int32 = -70000
		binary.BigEndian.PutUint32(b, uint32(neg))
		def := config.RegisterDef{RegisterAddress: 200, ModbusSize: 2, ScaleFactor: 1, DataType: "int32"}
		got, err := Decode(b, 200, def)
		if err != nil {
			t.Fatal(err)
		}
		if got != -70000 {
			t.Errorf("got %v, want -70000", got)
		}
	})

	t.Run("offset out of range", func(t *testing.T) {
		def := config.RegisterDef{RegisterAddress: 999, ModbusSize: 1, DataType: "uint16"}
		if _, err := Decode(block, 100, def); err == nil {
			t.Fatal("expected an out-of-range error")
		}
	})

	t.Run("unsupported data type", func(t *testing.T) {
		def := config.RegisterDef{RegisterAddress: 100, ModbusSize: 1, DataType: "bogus"}
		if _, err := Decode(block, 100, def); err == nil {
			t.Fatal("expected an unsupported dataType error")
		}
	})

	t.Run("rounds to default 2 decimals when outputDecimals unset", func(t *testing.T) {
		def := config.RegisterDef{RegisterAddress: 100, ModbusSize: 1, ScaleFactor: 0.001, DataType: "uint16"}
		got, err := Decode(block, 100, def) // 1234 * 0.001 = 1.234 -> 1.23
		if err != nil {
			t.Fatal(err)
		}
		if got != 1.23 {
			t.Errorf("got %v, want 1.23", got)
		}
	})

	t.Run("outputDecimals overrides the default", func(t *testing.T) {
		three := 3
		def := config.RegisterDef{RegisterAddress: 100, ModbusSize: 1, ScaleFactor: 0.001, DataType: "uint16", OutputDecimals: &three}
		got, err := Decode(block, 100, def) // 1234 * 0.001 = 1.234, kept at 3 decimals
		if err != nil {
			t.Fatal(err)
		}
		if got != 1.234 {
			t.Errorf("got %v, want 1.234", got)
		}
	})

	t.Run("outputDecimals of 0 rounds to a whole number", func(t *testing.T) {
		zero := 0
		def := config.RegisterDef{RegisterAddress: 100, ModbusSize: 1, ScaleFactor: 0.001, DataType: "uint16", OutputDecimals: &zero}
		got, err := Decode(block, 100, def) // 1234 * 0.001 = 1.234 -> 1
		if err != nil {
			t.Fatal(err)
		}
		if got != 1 {
			t.Errorf("got %v, want 1", got)
		}
	})
}
