// Package ebpf provides eBPF program loading and management.
package ebpf

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sync"

	"github.com/rs/zerolog/log"
)

// To generate eBPF bindings (requires clang and kernel headers):
// go generate ./...
//
//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags "-O2 -g -Wall" flowtracker ../../ebpf/flow_tracker.c
//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags "-O2 -g -Wall" egressmon ../../ebpf/egress_monitor.c

// FlowKey identifies a unique network flow.
type FlowKey struct {
	SrcIP    uint32
	DstIP    uint32
	SrcPort  uint16
	DstPort  uint16
	Protocol uint8
	Pad      [3]uint8
}

// FlowMetrics contains flow statistics.
type FlowMetrics struct {
	BytesSent       uint64
	BytesReceived   uint64
	PacketsSent     uint64
	PacketsReceived uint64
	StartTimeNs     uint64
	LastSeenNs      uint64
	PID             uint32
	UID             uint32
	Comm            [16]byte
}

// FlowEvent is sent from eBPF to userspace.
type FlowEvent struct {
	Key       FlowKey
	Metrics   FlowMetrics
	EventType uint8 // 0 = update, 1 = close
	Direction uint8 // 0 = egress, 1 = ingress
	Pad       [6]uint8
}

// EgressEvent represents egress traffic event.
type EgressEvent struct {
	SrcIP       uint32
	DstIP       uint32
	SrcPort     uint16
	DstPort     uint16
	Protocol    uint8
	Pad         [3]uint8
	Bytes       uint64
	TimestampNs uint64
	PID         uint32
	Pad2        uint32
}

// Loader manages eBPF program loading and lifecycle.
// Note: This is a stub implementation for development without eBPF.
// Real eBPF loading requires kernel support and generated code.
type Loader struct {
	mu              sync.RWMutex
	clusterCIDRs    []net.IPNet
	flowEventChan   chan FlowEvent
	egressEventChan chan EgressEvent
	stopChan        chan struct{}
	running         bool
	stubMode        bool
}

// NewLoader creates a new eBPF loader.
func NewLoader() *Loader {
	return &Loader{
		flowEventChan:   make(chan FlowEvent, 10000),
		egressEventChan: make(chan EgressEvent, 10000),
		stopChan:        make(chan struct{}),
		clusterCIDRs:    make([]net.IPNet, 0),
		stubMode:        true, // Default to stub mode for development
	}
}

// SetClusterCIDRs configures the cluster CIDR ranges for egress detection.
func (l *Loader) SetClusterCIDRs(cidrs []string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.clusterCIDRs = make([]net.IPNet, 0, len(cidrs))
	for _, cidr := range cidrs {
		_, ipnet, err := net.ParseCIDR(cidr)
		if err != nil {
			return fmt.Errorf("invalid CIDR %s: %w", cidr, err)
		}
		l.clusterCIDRs = append(l.clusterCIDRs, *ipnet)
	}
	return nil
}

// LoadFlowTracker loads the flow tracking eBPF program.
// In stub mode, this just logs and returns nil.
func (l *Loader) LoadFlowTracker(cgroupPath string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.stubMode {
		log.Warn().Str("cgroup", cgroupPath).Msg("eBPF stub mode: flow tracker not loaded (no kernel support)")
		return nil
	}

	// Real eBPF loading would happen here with generated code
	return errors.New("eBPF not compiled - run 'go generate ./src/pkg/ebpf/...' with clang installed")
}

// LoadEgressMonitor loads the egress monitoring eBPF program.
// In stub mode, this just logs and returns nil.
func (l *Loader) LoadEgressMonitor(interfaceName string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.stubMode {
		log.Warn().Str("interface", interfaceName).Msg("eBPF stub mode: egress monitor not loaded (no kernel support)")
		return nil
	}

	// Real eBPF loading would happen here with generated code
	return errors.New("eBPF not compiled - run 'go generate ./src/pkg/ebpf/...' with clang installed")
}

// Start begins reading events from eBPF programs.
func (l *Loader) Start() error {
	l.mu.Lock()
	if l.running {
		l.mu.Unlock()
		return errors.New("loader already running")
	}
	l.running = true
	l.mu.Unlock()

	if l.stubMode {
		log.Warn().Msg("eBPF stub mode: no events will be collected")
	}

	log.Info().Bool("stub_mode", l.stubMode).Msg("eBPF loader started")
	return nil
}

// Stop stops the eBPF loader.
func (l *Loader) Stop() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.running {
		return nil
	}

	close(l.stopChan)
	l.running = false

	log.Info().Msg("eBPF loader stopped")
	return nil
}

// FlowEvents returns channel for flow events.
func (l *Loader) FlowEvents() <-chan FlowEvent {
	return l.flowEventChan
}

// EgressEvents returns channel for egress events.
func (l *Loader) EgressEvents() <-chan EgressEvent {
	return l.egressEventChan
}

// IsStubMode returns true if running in stub mode (no real eBPF).
func (l *Loader) IsStubMode() bool {
	return l.stubMode
}

// SetStubMode enables or disables stub mode.
func (l *Loader) SetStubMode(stub bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.stubMode = stub
}

// parseFlowEvent parses raw bytes into FlowEvent.
func parseFlowEvent(data []byte) (FlowEvent, error) {
	if len(data) < 88 {
		return FlowEvent{}, errors.New("flow event data too short")
	}

	var event FlowEvent
	event.Key.SrcIP = binary.LittleEndian.Uint32(data[0:4])
	event.Key.DstIP = binary.LittleEndian.Uint32(data[4:8])
	event.Key.SrcPort = binary.LittleEndian.Uint16(data[8:10])
	event.Key.DstPort = binary.LittleEndian.Uint16(data[10:12])
	event.Key.Protocol = data[12]

	offset := 16
	event.Metrics.BytesSent = binary.LittleEndian.Uint64(data[offset:])
	event.Metrics.BytesReceived = binary.LittleEndian.Uint64(data[offset+8:])
	event.Metrics.PacketsSent = binary.LittleEndian.Uint64(data[offset+16:])
	event.Metrics.PacketsReceived = binary.LittleEndian.Uint64(data[offset+24:])
	event.Metrics.StartTimeNs = binary.LittleEndian.Uint64(data[offset+32:])
	event.Metrics.LastSeenNs = binary.LittleEndian.Uint64(data[offset+40:])
	event.Metrics.PID = binary.LittleEndian.Uint32(data[offset+48:])
	event.Metrics.UID = binary.LittleEndian.Uint32(data[offset+52:])
	copy(event.Metrics.Comm[:], data[offset+56:offset+72])

	event.EventType = data[offset+72]
	event.Direction = data[offset+73]

	return event, nil
}

// parseEgressEvent parses raw bytes into EgressEvent.
func parseEgressEvent(data []byte) (EgressEvent, error) {
	if len(data) < 40 {
		return EgressEvent{}, errors.New("egress event data too short")
	}

	var event EgressEvent
	event.SrcIP = binary.LittleEndian.Uint32(data[0:4])
	event.DstIP = binary.LittleEndian.Uint32(data[4:8])
	event.SrcPort = binary.LittleEndian.Uint16(data[8:10])
	event.DstPort = binary.LittleEndian.Uint16(data[10:12])
	event.Protocol = data[12]
	event.Bytes = binary.LittleEndian.Uint64(data[16:24])
	event.TimestampNs = binary.LittleEndian.Uint64(data[24:32])
	event.PID = binary.LittleEndian.Uint32(data[32:36])

	return event, nil
}

// IPToString converts uint32 IP to string.
func IPToString(ip uint32) string {
	return fmt.Sprintf("%d.%d.%d.%d",
		byte(ip), byte(ip>>8), byte(ip>>16), byte(ip>>24))
}

// GetFlowStats returns current flow map statistics.
// In stub mode, returns empty map.
func (l *Loader) GetFlowStats() (map[string]FlowMetrics, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if l.stubMode {
		return make(map[string]FlowMetrics), nil
	}

	return nil, errors.New("flow tracker not loaded")
}

// GetEgressStats returns current egress byte counters.
// In stub mode, returns empty map.
func (l *Loader) GetEgressStats() (map[string]uint64, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if l.stubMode {
		return make(map[string]uint64), nil
	}

	return nil, errors.New("egress monitor not loaded")
}

// InjectFlowEvent allows injecting test events (for testing/demo).
func (l *Loader) InjectFlowEvent(event FlowEvent) {
	select {
	case l.flowEventChan <- event:
	default:
		log.Warn().Msg("flow event channel full")
	}
}

// InjectEgressEvent allows injecting test events (for testing/demo).
func (l *Loader) InjectEgressEvent(event EgressEvent) {
	select {
	case l.egressEventChan <- event:
	default:
		log.Warn().Msg("egress event channel full")
	}
}
