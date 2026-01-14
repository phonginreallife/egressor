// Package engine implements the FlowScope analytics engine.
package engine

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/egressor/egressor/src/internal/storage"
	"github.com/egressor/egressor/src/pkg/types"
)

// ServiceNode represents a service in the transfer graph.
type ServiceNode struct {
	ID                 string
	Namespace          string
	Name               string
	Kind               string
	FirstSeen          time.Time
	LastSeen           time.Time
	TotalBytesSent     uint64
	TotalBytesReceived uint64
	TotalConnections   uint64
	TotalEgressCostUSD float64
	Neighbors          map[string]*Edge
}

// Edge represents a transfer relationship between services.
type Edge struct {
	SourceID         string
	DestinationID    string
	TransferType     types.TransferType
	FirstSeen        time.Time
	LastSeen         time.Time
	TotalBytes       uint64
	TotalEvents      uint64
	TotalCostUSD     float64
	BytesPerHourBase float64
	CurrentRateRatio float64
}

// TransferGraph represents the service dependency graph.
type TransferGraph struct {
	nodes         map[string]*ServiceNode
	edges         map[string]*Edge
	externalNodes map[string]*ServiceNode
	mu            sync.RWMutex
}

// NewTransferGraph creates a new transfer graph.
func NewTransferGraph() *TransferGraph {
	return &TransferGraph{
		nodes:         make(map[string]*ServiceNode),
		edges:         make(map[string]*Edge),
		externalNodes: make(map[string]*ServiceNode),
	}
}

// AddFlow adds a flow to the graph.
func (g *TransferGraph) AddFlow(flow types.TransferFlow) {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Get or create source node
	srcID := flow.SourceIdentity.FullName()
	srcNode := g.getOrCreateNode(srcID, flow.SourceIdentity)
	srcNode.TotalBytesSent += flow.TotalBytes
	srcNode.TotalConnections += flow.EventCount
	srcNode.LastSeen = flow.WindowEnd

	// Get or create destination
	var dstID string
	if flow.DestinationIdentity != nil {
		dstID = flow.DestinationIdentity.FullName()
		dstNode := g.getOrCreateNode(dstID, *flow.DestinationIdentity)
		dstNode.TotalBytesReceived += flow.TotalBytes
		dstNode.LastSeen = flow.WindowEnd
	} else if flow.DestinationEndpoint != nil {
		dstID = "external:" + flow.DestinationEndpoint.IP
		if _, ok := g.externalNodes[dstID]; !ok {
			g.externalNodes[dstID] = &ServiceNode{
				ID:        dstID,
				Namespace: "external",
				Name:      flow.DestinationEndpoint.IP,
				FirstSeen: flow.WindowStart,
				Neighbors: make(map[string]*Edge),
			}
		}
		g.externalNodes[dstID].TotalBytesReceived += flow.TotalBytes
		g.externalNodes[dstID].LastSeen = flow.WindowEnd
	} else {
		dstID = "unknown"
	}

	// Get or create edge
	edgeID := srcID + "→" + dstID
	edge := g.getOrCreateEdge(edgeID, srcID, dstID, flow.Type)
	edge.TotalBytes += flow.TotalBytes
	edge.TotalEvents += flow.EventCount
	edge.LastSeen = flow.WindowEnd

	// Update neighbor reference
	srcNode.Neighbors[dstID] = edge
}

func (g *TransferGraph) getOrCreateNode(id string, identity types.ServiceIdentity) *ServiceNode {
	if node, ok := g.nodes[id]; ok {
		return node
	}

	node := &ServiceNode{
		ID:        id,
		Namespace: identity.Namespace,
		Name:      identity.Name,
		Kind:      identity.Kind,
		FirstSeen: time.Now(),
		LastSeen:  time.Now(),
		Neighbors: make(map[string]*Edge),
	}
	g.nodes[id] = node
	return node
}

func (g *TransferGraph) getOrCreateEdge(id, srcID, dstID string, transferType types.TransferType) *Edge {
	if edge, ok := g.edges[id]; ok {
		return edge
	}

	edge := &Edge{
		SourceID:      srcID,
		DestinationID: dstID,
		TransferType:  transferType,
		FirstSeen:     time.Now(),
		LastSeen:      time.Now(),
	}
	g.edges[id] = edge
	return edge
}

// GetNode returns a node by ID.
func (g *TransferGraph) GetNode(id string) *ServiceNode {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if node, ok := g.nodes[id]; ok {
		return node
	}
	return g.externalNodes[id]
}

// GetEdge returns an edge between two nodes.
func (g *TransferGraph) GetEdge(srcID, dstID string) *Edge {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.edges[srcID+"→"+dstID]
}

// GetTopTalkers returns services with highest bytes sent.
func (g *TransferGraph) GetTopTalkers(n int) []*ServiceNode {
	g.mu.RLock()
	defer g.mu.RUnlock()

	nodes := make([]*ServiceNode, 0, len(g.nodes))
	for _, node := range g.nodes {
		nodes = append(nodes, node)
	}

	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].TotalBytesSent > nodes[j].TotalBytesSent
	})

	if n > len(nodes) {
		n = len(nodes)
	}
	return nodes[:n]
}

// GetTopEdges returns edges with highest bytes.
func (g *TransferGraph) GetTopEdges(n int) []*Edge {
	g.mu.RLock()
	defer g.mu.RUnlock()

	edges := make([]*Edge, 0, len(g.edges))
	for _, edge := range g.edges {
		edges = append(edges, edge)
	}

	sort.Slice(edges, func(i, j int) bool {
		return edges[i].TotalBytes > edges[j].TotalBytes
	})

	if n > len(edges) {
		n = len(edges)
	}
	return edges[:n]
}

// GetEgressEdges returns all egress edges.
func (g *TransferGraph) GetEgressEdges() []*Edge {
	g.mu.RLock()
	defer g.mu.RUnlock()

	var edges []*Edge
	for _, edge := range g.edges {
		if edge.TransferType == types.TransferTypeEgress {
			edges = append(edges, edge)
		}
	}
	return edges
}

// GetCrossRegionEdges returns all cross-region edges.
func (g *TransferGraph) GetCrossRegionEdges() []*Edge {
	g.mu.RLock()
	defer g.mu.RUnlock()

	var edges []*Edge
	for _, edge := range g.edges {
		if edge.TransferType == types.TransferTypeCrossRegion {
			edges = append(edges, edge)
		}
	}
	return edges
}

// GetServiceGraph returns a subgraph for a specific service.
func (g *TransferGraph) GetServiceGraph(serviceID string, depth int) *TransferGraph {
	g.mu.RLock()
	defer g.mu.RUnlock()

	subgraph := NewTransferGraph()
	visited := make(map[string]bool)

	g.traverseService(subgraph, serviceID, depth, visited)
	return subgraph
}

func (g *TransferGraph) traverseService(subgraph *TransferGraph, serviceID string, depth int, visited map[string]bool) {
	if depth < 0 || visited[serviceID] {
		return
	}
	visited[serviceID] = true

	node := g.nodes[serviceID]
	if node == nil {
		return
	}

	subgraph.nodes[serviceID] = node

	for dstID, edge := range node.Neighbors {
		subgraph.edges[edge.SourceID+"→"+edge.DestinationID] = edge
		g.traverseService(subgraph, dstID, depth-1, visited)
	}
}

// GetStats returns graph statistics.
func (g *TransferGraph) GetStats() GraphStats {
	g.mu.RLock()
	defer g.mu.RUnlock()

	var totalBytes, egressBytes, crossRegionBytes uint64
	for _, edge := range g.edges {
		totalBytes += edge.TotalBytes
		if edge.TransferType == types.TransferTypeEgress {
			egressBytes += edge.TotalBytes
		}
		if edge.TransferType == types.TransferTypeCrossRegion {
			crossRegionBytes += edge.TotalBytes
		}
	}

	return GraphStats{
		TotalNodes:         len(g.nodes),
		TotalExternalNodes: len(g.externalNodes),
		TotalEdges:         len(g.edges),
		TotalBytes:         totalBytes,
		EgressBytes:        egressBytes,
		CrossRegionBytes:   crossRegionBytes,
	}
}

// GraphStats holds graph statistics.
type GraphStats struct {
	TotalNodes         int    `json:"total_nodes"`
	TotalExternalNodes int    `json:"total_external_nodes"`
	TotalEdges         int    `json:"total_edges"`
	TotalBytes         uint64 `json:"total_bytes"`
	EgressBytes        uint64 `json:"egress_bytes"`
	CrossRegionBytes   uint64 `json:"cross_region_bytes"`
}

// ToJSON exports graph to JSON-serializable format.
func (g *TransferGraph) ToJSON() GraphJSON {
	g.mu.RLock()
	defer g.mu.RUnlock()

	nodes := make([]NodeJSON, 0, len(g.nodes))
	for _, n := range g.nodes {
		nodes = append(nodes, NodeJSON{
			ID:                 n.ID,
			Namespace:          n.Namespace,
			Name:               n.Name,
			TotalBytesSent:     n.TotalBytesSent,
			TotalBytesReceived: n.TotalBytesReceived,
			TotalConnections:   n.TotalConnections,
		})
	}

	edges := make([]EdgeJSON, 0, len(g.edges))
	for _, e := range g.edges {
		edges = append(edges, EdgeJSON{
			Source:       e.SourceID,
			Target:       e.DestinationID,
			TransferType: string(e.TransferType),
			TotalBytes:   e.TotalBytes,
			TotalEvents:  e.TotalEvents,
			CostUSD:      e.TotalCostUSD,
		})
	}

	return GraphJSON{
		Nodes: nodes,
		Edges: edges,
		Stats: g.GetStats(),
	}
}

// NodeJSON is JSON representation of a node.
type NodeJSON struct {
	ID                 string `json:"id"`
	Namespace          string `json:"namespace"`
	Name               string `json:"name"`
	TotalBytesSent     uint64 `json:"total_bytes_sent"`
	TotalBytesReceived uint64 `json:"total_bytes_received"`
	TotalConnections   uint64 `json:"total_connections"`
}

// EdgeJSON is JSON representation of an edge.
type EdgeJSON struct {
	Source       string  `json:"source"`
	Target       string  `json:"target"`
	TransferType string  `json:"transfer_type"`
	TotalBytes   uint64  `json:"total_bytes"`
	TotalEvents  uint64  `json:"total_events"`
	CostUSD      float64 `json:"cost_usd"`
}

// GraphJSON is the full graph JSON structure.
type GraphJSON struct {
	Nodes []NodeJSON `json:"nodes"`
	Edges []EdgeJSON `json:"edges"`
	Stats GraphStats `json:"stats"`
}

// GraphEngine manages the transfer graph with storage backing.
type GraphEngine struct {
	graph   *TransferGraph
	storage *storage.ClickHouseStore
}

// NewGraphEngine creates a new graph engine.
func NewGraphEngine(store *storage.ClickHouseStore) *GraphEngine {
	return &GraphEngine{
		graph:   NewTransferGraph(),
		storage: store,
	}
}

// LoadFromStorage loads graph data from storage.
func (e *GraphEngine) LoadFromStorage(ctx context.Context, start, end time.Time) error {
	if e.storage == nil {
		return nil
	}

	results, err := e.storage.QueryFlows(ctx, storage.FlowQuery{
		Start: start,
		End:   end,
		Limit: 100000,
	})
	if err != nil {
		return fmt.Errorf("querying flows: %w", err)
	}

	for _, r := range results {
		flow := types.TransferFlow{
			ID: uuid.New(),
			SourceIdentity: types.ServiceIdentity{
				Namespace: r.SrcNamespace,
				Name:      r.SrcService,
			},
			TotalBytes:  r.TotalBytes,
			WindowStart: start,
			WindowEnd:   end,
		}

		if r.DstService != "" {
			flow.DestinationIdentity = &types.ServiceIdentity{
				Namespace: r.DstNamespace,
				Name:      r.DstService,
			}
		} else if r.DstExternal != "" {
			flow.DestinationEndpoint = &types.Endpoint{
				IP:         r.DstExternal,
				Type:       types.EndpointTypeExternal,
				IsInternet: true,
			}
		}

		flow.Type = types.TransferType(r.TransferType)
		e.graph.AddFlow(flow)
	}

	log.Info().
		Int("flows", len(results)).
		Time("start", start).
		Time("end", end).
		Msg("Graph loaded from storage")

	return nil
}

// GetGraph returns the transfer graph.
func (e *GraphEngine) GetGraph() *TransferGraph {
	return e.graph
}

// AddFlow adds a flow to the graph.
func (e *GraphEngine) AddFlow(flow types.TransferFlow) {
	e.graph.AddFlow(flow)
}

// GetStats returns graph statistics.
func (e *GraphEngine) GetStats() GraphStats {
	return e.graph.GetStats()
}

// ToJSON exports graph to JSON-serializable format.
func (e *GraphEngine) ToJSON() GraphJSON {
	return e.graph.ToJSON()
}

// GetTopNodes returns nodes with highest total bytes.
func (e *GraphEngine) GetTopNodes(n int) []*ServiceNode {
	return e.graph.GetTopTalkers(n)
}

// GetTopEdges returns edges with highest bytes.
func (e *GraphEngine) GetTopEdges(n int) []*Edge {
	return e.graph.GetTopEdges(n)
}
