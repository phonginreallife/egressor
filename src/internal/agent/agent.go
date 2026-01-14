// Package agent implements the FlowScope node agent.
package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/egressor/egressor/src/pkg/ebpf"
	"github.com/egressor/egressor/src/pkg/types"
)

// Config holds agent configuration.
type Config struct {
	CollectorEndpoint string
	CgroupPath        string
	NodeName          string
	ClusterName       string
	ClusterCIDRs      []string
	ExportInterval    time.Duration
}

// Agent is the FlowScope node agent.
type Agent struct {
	cfg       Config
	loader    *ebpf.Loader
	enricher  *K8sEnricher
	exporter  *Exporter
	mu        sync.RWMutex
	running   bool
	stopChan  chan struct{}
	events    chan types.TransferEvent
}

// New creates a new agent.
func New(cfg Config) (*Agent, error) {
	loader := ebpf.NewLoader()
	if err := loader.SetClusterCIDRs(cfg.ClusterCIDRs); err != nil {
		return nil, fmt.Errorf("setting cluster CIDRs: %w", err)
	}

	enricher, err := NewK8sEnricher()
	if err != nil {
		return nil, fmt.Errorf("creating k8s enricher: %w", err)
	}

	return &Agent{
		cfg:      cfg,
		loader:   loader,
		enricher: enricher,
		stopChan: make(chan struct{}),
		events:   make(chan types.TransferEvent, 10000),
	}, nil
}

// Start starts the agent.
func (a *Agent) Start(ctx context.Context) error {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return fmt.Errorf("agent already running")
	}
	a.running = true
	a.mu.Unlock()

	// Load eBPF programs
	log.Info().Str("cgroup", a.cfg.CgroupPath).Msg("Loading eBPF programs")
	if err := a.loader.LoadFlowTracker(a.cfg.CgroupPath); err != nil {
		log.Warn().Err(err).Msg("Failed to load flow tracker (may need root)")
	}

	// Start eBPF event reading
	if err := a.loader.Start(); err != nil {
		return fmt.Errorf("starting eBPF loader: %w", err)
	}

	// Connect to collector
	log.Info().Str("endpoint", a.cfg.CollectorEndpoint).Msg("Connecting to collector")
	exporter, err := NewExporter(a.cfg.CollectorEndpoint)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to connect to collector")
	} else {
		a.exporter = exporter
	}

	// Start background workers
	go a.processFlowEvents(ctx)
	go a.processEgressEvents(ctx)
	go a.exportLoop(ctx)

	log.Info().
		Str("node", a.cfg.NodeName).
		Str("cluster", a.cfg.ClusterName).
		Msg("Agent started")

	return nil
}

// Stop stops the agent.
func (a *Agent) Stop(ctx context.Context) error {
	a.mu.Lock()
	if !a.running {
		a.mu.Unlock()
		return nil
	}
	a.running = false
	close(a.stopChan)
	a.mu.Unlock()

	// Stop eBPF loader
	if err := a.loader.Stop(); err != nil {
		log.Error().Err(err).Msg("Error stopping eBPF loader")
	}

	// Close exporter
	if a.exporter != nil {
		if err := a.exporter.Close(); err != nil {
			log.Error().Err(err).Msg("Error closing exporter")
		}
	}

	return nil
}

// processFlowEvents processes events from flow tracker.
func (a *Agent) processFlowEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-a.stopChan:
			return
		case event := <-a.loader.FlowEvents():
			transferEvent := a.convertFlowEvent(event)
			if transferEvent != nil {
				a.enrichAndQueue(*transferEvent)
			}
		}
	}
}

// processEgressEvents processes events from egress monitor.
func (a *Agent) processEgressEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-a.stopChan:
			return
		case event := <-a.loader.EgressEvents():
			transferEvent := a.convertEgressEvent(event)
			if transferEvent != nil {
				a.enrichAndQueue(*transferEvent)
			}
		}
	}
}

// convertFlowEvent converts eBPF flow event to transfer event.
func (a *Agent) convertFlowEvent(event ebpf.FlowEvent) *types.TransferEvent {
	srcIP := ebpf.IPToString(event.Key.SrcIP)
	dstIP := ebpf.IPToString(event.Key.DstIP)

	protocol := "TCP"
	if event.Key.Protocol == 17 {
		protocol = "UDP"
	}

	direction := types.DirectionOutbound
	if event.Direction == 1 {
		direction = types.DirectionInbound
	}

	return &types.TransferEvent{
		ID: uuid.New(),
		Source: types.Endpoint{
			Type: types.EndpointTypePod,
			IP:   srcIP,
			Port: event.Key.SrcPort,
		},
		Destination: types.Endpoint{
			Type: types.EndpointTypeUnknown,
			IP:   dstIP,
			Port: event.Key.DstPort,
		},
		Protocol:        protocol,
		Direction:       direction,
		Type:            types.TransferTypePodToPod,
		BytesSent:       event.Metrics.BytesSent,
		BytesReceived:   event.Metrics.BytesReceived,
		PacketsSent:     event.Metrics.PacketsSent,
		PacketsReceived: event.Metrics.PacketsReceived,
		Timestamp:       time.Now(),
		DurationNs:      event.Metrics.LastSeenNs - event.Metrics.StartTimeNs,
	}
}

// convertEgressEvent converts eBPF egress event to transfer event.
func (a *Agent) convertEgressEvent(event ebpf.EgressEvent) *types.TransferEvent {
	srcIP := ebpf.IPToString(event.SrcIP)
	dstIP := ebpf.IPToString(event.DstIP)

	protocol := "TCP"
	if event.Protocol == 17 {
		protocol = "UDP"
	}

	return &types.TransferEvent{
		ID: uuid.New(),
		Source: types.Endpoint{
			Type: types.EndpointTypePod,
			IP:   srcIP,
			Port: event.SrcPort,
		},
		Destination: types.Endpoint{
			Type:       types.EndpointTypeExternal,
			IP:         dstIP,
			Port:       event.DstPort,
			IsInternet: true,
		},
		Protocol:      protocol,
		Direction:     types.DirectionOutbound,
		Type:          types.TransferTypeEgress,
		BytesSent:     event.Bytes,
		Timestamp:     time.Unix(0, int64(event.TimestampNs)),
	}
}

// enrichAndQueue enriches event with K8s metadata and queues it.
func (a *Agent) enrichAndQueue(event types.TransferEvent) {
	// Enrich source
	if identity := a.enricher.GetIdentity(event.Source.IP); identity != nil {
		event.Source.Identity = identity
		event.Source.Type = types.EndpointTypePod
	}

	// Enrich destination
	if event.Destination.Type != types.EndpointTypeExternal {
		if identity := a.enricher.GetIdentity(event.Destination.IP); identity != nil {
			event.Destination.Identity = identity
			event.Destination.Type = types.EndpointTypePod
		}
	}

	// Classify transfer type
	event.Type = classifyTransferType(event)

	// Add node/cluster metadata
	if event.Source.Identity != nil {
		event.Source.Identity.NodeName = a.cfg.NodeName
		event.Source.Identity.Cluster = a.cfg.ClusterName
	}

	select {
	case a.events <- event:
	default:
		log.Warn().Msg("Event queue full, dropping event")
	}
}

// classifyTransferType determines the transfer type.
func classifyTransferType(event types.TransferEvent) types.TransferType {
	if event.Destination.IsInternet || event.Destination.Type == types.EndpointTypeExternal {
		return types.TransferTypeEgress
	}
	if event.Source.Type == types.EndpointTypeExternal {
		return types.TransferTypeIngress
	}

	src := event.Source.Identity
	dst := event.Destination.Identity

	if src != nil && dst != nil {
		if src.Region != "" && dst.Region != "" && src.Region != dst.Region {
			return types.TransferTypeCrossRegion
		}
		if src.AvailabilityZone != "" && dst.AvailabilityZone != "" && src.AvailabilityZone != dst.AvailabilityZone {
			return types.TransferTypeCrossAZ
		}
	}

	return types.TransferTypePodToPod
}

// exportLoop periodically exports events to collector.
func (a *Agent) exportLoop(ctx context.Context) {
	ticker := time.NewTicker(a.cfg.ExportInterval)
	defer ticker.Stop()

	var batch []types.TransferEvent

	for {
		select {
		case <-ctx.Done():
			return
		case <-a.stopChan:
			// Export remaining events
			if len(batch) > 0 && a.exporter != nil {
				a.exporter.Export(ctx, batch)
			}
			return
		case event := <-a.events:
			batch = append(batch, event)
			// Export if batch is large enough
			if len(batch) >= 1000 {
				if a.exporter != nil {
					go a.exporter.Export(ctx, batch)
				}
				batch = nil
			}
		case <-ticker.C:
			if len(batch) > 0 && a.exporter != nil {
				go a.exporter.Export(ctx, batch)
				batch = nil
			}
		}
	}
}

// Exporter exports events to the collector.
type Exporter struct {
	conn   *grpc.ClientConn
	// client pb.CollectorClient // Would use generated proto client
}

// NewExporter creates a new exporter.
func NewExporter(endpoint string) (*Exporter, error) {
	conn, err := grpc.Dial(endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("connecting to collector: %w", err)
	}

	return &Exporter{
		conn: conn,
		// client: pb.NewCollectorClient(conn),
	}, nil
}

// Export exports a batch of events.
func (e *Exporter) Export(ctx context.Context, events []types.TransferEvent) error {
	if e.conn == nil {
		return fmt.Errorf("not connected")
	}

	log.Debug().Int("count", len(events)).Msg("Exporting events")
	// Would serialize and send via gRPC
	// return e.client.IngestEvents(ctx, &pb.IngestRequest{Events: events})
	return nil
}

// Close closes the exporter.
func (e *Exporter) Close() error {
	if e.conn != nil {
		return e.conn.Close()
	}
	return nil
}
