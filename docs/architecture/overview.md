# FlowScope Architecture Overview

## System Layers

### 1. Traffic Collection Layer (eBPF)

The heart of FlowScope. Uses eBPF for lowest overhead, kernel-level visibility.

**Components:**
- `flow_tracker.c` - Tracks TCP/UDP connections at socket level
- `egress_monitor.c` - Specifically monitors outbound (egress) traffic

**Why eBPF:**
- Lowest possible overhead (~1% CPU)
- Sees real traffic, not proxied
- Production-proven (Cilium, Falco, etc.)
- No application changes required

### 2. Agent / Node Daemon (Go)

Runs as DaemonSet on each Kubernetes node.

**Responsibilities:**
- Load eBPF programs
- Collect flow events from kernel
- Enrich with Kubernetes metadata (pod → service → namespace)
- Batch and export to collector

**Key Features:**
- Pod IP to service identity mapping
- Owner reference resolution (Pod → ReplicaSet → Deployment)
- Node and AZ awareness

### 3. Transport Layer (OpenTelemetry + gRPC)

Standard, future-proof telemetry transport.

**Protocol:**
- gRPC for efficiency
- OTel-compatible format
- Supports batching and compression

### 4. Collector / Ingestion (Go)

Central event collection and normalization.

**Responsibilities:**
- Receive events from all agents
- Normalize into TransferEvent schema
- Batch write to ClickHouse
- Maintain real-time metrics

### 5. Data Engine

**ClickHouse** - Analytics store for high-volume flow data
- Columnar storage optimized for time-series
- Automatic hourly aggregation via materialized views
- 30-day retention for raw events

**PostgreSQL** - Metadata and configuration
- Pricing rules
- Baseline configurations
- User preferences

### 6. Transfer Graph Engine (Go)

Builds and maintains the service dependency graph.

**Features:**
- Real-time graph updates
- Top talkers/receivers
- Egress/cross-region edge identification
- Subgraph extraction for specific services

### 7. Cost Attribution Engine (Go)

Maps traffic to cloud pricing.

**Supported Cost Categories:**
- Internet egress (tiered pricing)
- Cross-AZ transfer
- Cross-region transfer
- NAT Gateway
- VPC Peering

**Pricing Sources:**
- Built-in AWS pricing tables
- Configurable custom rules
- API integration for dynamic pricing

### 8. Baseline Engine (Go)

Behavioral analysis and anomaly detection.

**Baseline Metrics:**
- Bytes per hour (mean, stddev, percentiles)
- Hourly patterns (24-hour cycle)
- Daily patterns (7-day cycle)

**Anomaly Types:**
- Spike (sudden increase)
- Slow burn (gradual increase)
- New endpoint (previously unseen)
- Leak (continuous low-volume)

### 9. Intelligence Layer (Claude)

AI-powered analysis via Anthropic Claude API.

**Capabilities:**
- Natural language investigation
- Cost increase explanation
- Optimization suggestions
- Anomaly investigation

**Data Sent to Claude:**
- Summaries and statistics
- Anomaly details
- Cost breakdowns
- **Never raw traffic data**

### 10. API Server (Go)

REST + gRPC API for all client interactions.

**Endpoints:**
- Graph queries
- Cost attribution
- Anomaly management
- Claude integration

### 11. Frontend (Next.js)

Modern dashboard for visualization and interaction.

**Features:**
- Real-time transfer graph
- Cost trends and breakdown
- Anomaly list with AI summaries
- Claude chat interface

## Data Flow

```
[Pods] → [eBPF] → [Agent] → [Collector] → [ClickHouse]
                                              ↓
                                        [Engines]
                                              ↓
                                         [Claude]
                                              ↓
                                          [API]
                                              ↓
                                        [Frontend]
```

## Key Design Decisions

1. **Go for backend** - Concurrency, static binaries, eBPF ecosystem
2. **eBPF for collection** - Lowest overhead, kernel visibility
3. **ClickHouse for analytics** - Best for high-volume time-series
4. **Claude for AI** - Best reasoning capabilities
5. **Next.js for frontend** - Modern React with SSR

## Scalability

- **Agents**: Scale automatically with nodes (DaemonSet)
- **Collector**: Horizontal scaling with load balancer
- **ClickHouse**: Cluster mode for high throughput
- **API**: Stateless, horizontally scalable
