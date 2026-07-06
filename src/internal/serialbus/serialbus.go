// Package serialbus manages the Modbus RTU serial links declared in
// config.json. Each link is shared by every device configured to use it,
// and reconnects lazily the next time it is used after a failed read, so a
// disconnected or misbehaving serial port does not require restarting the
// whole application.
package serialbus

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/grid-x/modbus"

	"solis2mqtt/src/internal/config"
)

// Bus is one physical serial link, guarded by a mutex since Modbus RTU is a
// half-duplex, single-conversation-at-a-time protocol: only one read may be
// in flight on a link at a time, even if several devices share it.
type Bus struct {
	cfg     config.Link
	timeout time.Duration

	mu      sync.Mutex
	handler *modbus.RTUClientHandler
	client  modbus.Client
}

func newBus(cfg config.Link, timeout time.Duration) *Bus {
	return &Bus{cfg: cfg, timeout: timeout}
}

func parityChar(parity string) string {
	switch strings.ToLower(parity) {
	case "even":
		return "E"
	case "odd":
		return "O"
	default:
		return "N"
	}
}

func (b *Bus) ensureConnectedLocked(ctx context.Context) error {
	if b.handler != nil {
		return nil
	}

	handler := modbus.NewRTUClientHandler(b.cfg.LinkName)
	handler.BaudRate = b.cfg.BaudRate
	handler.DataBits = b.cfg.DataBits
	handler.Parity = parityChar(b.cfg.Parity)
	handler.StopBits = b.cfg.StopBits
	handler.Timeout = b.timeout

	if err := handler.Connect(ctx); err != nil {
		return err
	}

	b.handler = handler
	b.client = modbus.NewClient(handler)
	return nil
}

func (b *Bus) closeLocked() {
	if b.handler != nil {
		b.handler.Close()
		b.handler = nil
		b.client = nil
	}
}

// Read performs a single Modbus read (function code 3 = holding registers,
// 4 = input registers) for slaveID against this link. On failure the
// underlying connection is closed so the next call reconnects from scratch.
func (b *Bus) Read(ctx context.Context, slaveID byte, functionCode int, address, quantity uint16) ([]byte, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if err := b.ensureConnectedLocked(ctx); err != nil {
		return nil, fmt.Errorf("connect %s: %w", b.cfg.LinkName, err)
	}

	b.handler.SlaveID = slaveID

	var data []byte
	var err error
	switch functionCode {
	case 3:
		data, err = b.client.ReadHoldingRegisters(ctx, address, quantity)
	case 4:
		data, err = b.client.ReadInputRegisters(ctx, address, quantity)
	default:
		return nil, fmt.Errorf("unsupported Modbus function code %d", functionCode)
	}
	if err != nil {
		b.closeLocked()
		return nil, err
	}

	return data, nil
}

// Close releases the underlying serial port, if open.
func (b *Bus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closeLocked()
}

// Manager owns one Bus per configured link.
type Manager struct {
	buses map[string]*Bus
}

// NewManager builds a Bus for every link in links. Connections are opened
// lazily on first use, not here.
func NewManager(links []config.Link, timeout time.Duration) *Manager {
	m := &Manager{buses: make(map[string]*Bus, len(links))}
	for _, l := range links {
		m.buses[l.LinkID] = newBus(l, timeout)
	}
	return m
}

// Bus returns the bus registered for linkID.
func (m *Manager) Bus(linkID string) (*Bus, bool) {
	b, ok := m.buses[linkID]
	return b, ok
}

// CloseAll closes every managed bus.
func (m *Manager) CloseAll() {
	for _, b := range m.buses {
		b.Close()
	}
}
