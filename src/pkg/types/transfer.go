// Package types defines core data types for FlowScope.
package types

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// EndpointType classifies the type of network endpoint.
type EndpointType string

const (
	EndpointTypePod      EndpointType = "pod"
	EndpointTypeService  EndpointType = "service"
	EndpointTypeNode     EndpointType = "node"
	EndpointTypeExternal EndpointType = "external"
	EndpointTypeUnknown  EndpointType = "unknown"
)

// TransferType classifies the type of data transfer.
type TransferType string

const (
	TransferTypePodToPod         TransferType = "pod_to_pod"
	TransferTypePodToService     TransferType = "pod_to_service"
	TransferTypeServiceToService TransferType = "service_to_service"
	TransferTypeEgress           TransferType = "egress"
	TransferTypeIngress          TransferType = "ingress"
	TransferTypeCrossAZ          TransferType = "cross_az"
	TransferTypeCrossRegion      TransferType = "cross_region"
	TransferTypeCrossCluster     TransferType = "cross_cluster"
)

// Direction indicates traffic direction relative to observer.
type Direction string

const (
	DirectionInbound  Direction = "inbound"
	DirectionOutbound Direction = "outbound"
)

// ServiceIdentity represents the identity of a Kubernetes workload.
type ServiceIdentity struct {
	Namespace        string            `json:"namespace"`
	Name             string            `json:"name"`
	Kind             string            `json:"kind"` // Deployment, StatefulSet, etc.
	Version          string            `json:"version,omitempty"`
	Team             string            `json:"team,omitempty"`
	Environment      string            `json:"environment,omitempty"`
	PodName          string            `json:"pod_name,omitempty"`
	NodeName         string            `json:"node_name,omitempty"`
	Cluster          string            `json:"cluster,omitempty"`
	AvailabilityZone string            `json:"availability_zone,omitempty"`
	Region           string            `json:"region,omitempty"`
	Labels           map[string]string `json:"labels,omitempty"`
}

// FullName returns the fully qualified service name.
func (s ServiceIdentity) FullName() string {
	return fmt.Sprintf("%s/%s", s.Namespace, s.Name)
}

// Endpoint represents a network endpoint in a transfer.
type Endpoint struct {
	Type             EndpointType     `json:"type"`
	IP               string           `json:"ip"`
	Port             uint16           `json:"port"`
	Identity         *ServiceIdentity `json:"identity,omitempty"`
	Hostname         string           `json:"hostname,omitempty"`
	DNSNames         []string         `json:"dns_names,omitempty"`
	Region           string           `json:"region,omitempty"`
	AvailabilityZone string           `json:"availability_zone,omitempty"`
	CloudProvider    string           `json:"cloud_provider,omitempty"`
	IsInternet       bool             `json:"is_internet"`
	IsCloudService   bool             `json:"is_cloud_service"`
	CloudServiceName string           `json:"cloud_service_name,omitempty"`
}

// TransferEvent represents a single data transfer event between two endpoints.
type TransferEvent struct {
	ID          uuid.UUID    `json:"id"`
	Source      Endpoint     `json:"source"`
	Destination Endpoint     `json:"destination"`
	Protocol    string       `json:"protocol"` // TCP, UDP, HTTP, gRPC
	Direction   Direction    `json:"direction"`
	Type        TransferType `json:"type"`

	// Metrics
	BytesSent       uint64 `json:"bytes_sent"`
	BytesReceived   uint64 `json:"bytes_received"`
	PacketsSent     uint64 `json:"packets_sent"`
	PacketsReceived uint64 `json:"packets_received"`

	// Timing
	Timestamp  time.Time `json:"timestamp"`
	DurationNs uint64    `json:"duration_ns,omitempty"`

	// Request context
	HTTPMethod     string `json:"http_method,omitempty"`
	HTTPPath       string `json:"http_path,omitempty"`
	HTTPStatusCode int    `json:"http_status_code,omitempty"`
	GRPCMethod     string `json:"grpc_method,omitempty"`

	// Correlation
	TraceID string `json:"trace_id,omitempty"`
	SpanID  string `json:"span_id,omitempty"`

	// Metadata
	Labels map[string]string `json:"labels,omitempty"`
}

// TotalBytes returns total bytes transferred.
func (e TransferEvent) TotalBytes() uint64 {
	return e.BytesSent + e.BytesReceived
}

// IsExternal checks if transfer involves an external endpoint.
func (e TransferEvent) IsExternal() bool {
	return e.Source.Type == EndpointTypeExternal || e.Destination.Type == EndpointTypeExternal
}

// TransferFlow represents aggregated transfer data over a time window.
type TransferFlow struct {
	ID                  uuid.UUID        `json:"id"`
	SourceIdentity      ServiceIdentity  `json:"source_identity"`
	DestinationIdentity *ServiceIdentity `json:"destination_identity,omitempty"`
	DestinationEndpoint *Endpoint        `json:"destination_endpoint,omitempty"`
	Type                TransferType     `json:"type"`

	// Aggregated metrics
	TotalBytes   uint64 `json:"total_bytes"`
	TotalPackets uint64 `json:"total_packets"`
	EventCount   uint64 `json:"event_count"`

	// Time window
	WindowStart time.Time `json:"window_start"`
	WindowEnd   time.Time `json:"window_end"`

	// Rate statistics
	BytesPerSecondAvg float64 `json:"bytes_per_second_avg"`
	BytesPerSecondMax float64 `json:"bytes_per_second_max"`
	BytesPerSecondP99 float64 `json:"bytes_per_second_p99"`

	// Breakdown
	ByHTTPPath   map[string]uint64 `json:"by_http_path,omitempty"`
	ByGRPCMethod map[string]uint64 `json:"by_grpc_method,omitempty"`
}

// FlowKey returns a unique identifier for this flow pair.
func (f TransferFlow) FlowKey() string {
	src := f.SourceIdentity.FullName()
	var dst string
	if f.DestinationIdentity != nil {
		dst = f.DestinationIdentity.FullName()
	} else if f.DestinationEndpoint != nil {
		dst = f.DestinationEndpoint.IP
	} else {
		dst = "unknown"
	}
	return fmt.Sprintf("%s→%s", src, dst)
}

// DurationSeconds returns the window duration in seconds.
func (f TransferFlow) DurationSeconds() float64 {
	return f.WindowEnd.Sub(f.WindowStart).Seconds()
}
