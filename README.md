# Egressor

**Data Transfer Intelligence Platform**

Detect, explain, and reduce unexpected or excessive data transfer in distributed systems (Kubernetes / cloud / services).

## 🎯 What Egressor Solves

- **Hidden egress costs** - Understand exactly where your cloud bill is going
- **Noisy network behavior** - Identify unnecessary service-to-service communication
- **Hard-to-trace data movement** - Visualize cross-service, cross-region, cross-AZ traffic
- **"Why is this suddenly expensive / slow?"** - Get AI-powered explanations for anomalies

## 🧱 Core Capabilities

### 1️⃣ Data Transfer Tracing

Collect and correlate:
- Pod ↔ Pod traffic
- Pod ↔ External endpoints
- Service ↔ Service communication
- Region ↔ Region transfers

### 2️⃣ Behavior Profiling

Build baselines and detect:
- **Spikes** - Sudden traffic increases
- **Slow burns** - Gradual increases over time
- **New endpoints** - Previously unseen destinations
- **Leaks** - Continuous low-volume unexpected transfers

### 3️⃣ Cost Attribution

Map traffic → cloud pricing → who caused it

### 4️⃣ Claude as the Intelligence Layer

Example queries:
- *"Why did our egress cost triple yesterday?"*
- *"Which services changed behavior after version 2.1?"*
- *"Show me the top 5 unnecessary transfers."*

## 🏗️ Architecture

```
┌─────────────────┐
│  Agents (eBPF)  │  ← Per-node DaemonSet
│   Go + eBPF/C   │
└────────┬────────┘
         │ gRPC/OTel
         ▼
┌─────────────────┐
│    Collector    │  ← Event ingestion
│       Go        │
└────────┬────────┘
         │
    ┌────┴────┐
    ▼         ▼
┌───────┐ ┌───────┐
│ Click │ │Postgre│  ← Storage
│ House │ │  SQL  │
└───┬───┘ └───────┘
    │
    ▼
┌─────────────────┐
│  Graph Engine   │  ← Analytics
│  Cost Engine    │
│ Baseline Engine │
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ Claude Service  │  ← AI Intelligence
│    (Anthropic)  │
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│   API Server    │  ← REST + gRPC
│       Go        │
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│    Frontend     │  ← Dashboard
│    Next.js      │
└─────────────────┘
```

## 📁 Project Structure

```
egressor/
├── src/                        # Backend (Go)
│   ├── cmd/                    # Application entrypoints
│   │   ├── agent/              # Node agent
│   │   ├── collector/          # Event collector
│   │   └── api/                # API server
│   ├── ebpf/                   # eBPF programs (C)
│   │   ├── flow_tracker.c
│   │   └── egress_monitor.c
│   ├── pkg/                    # Public packages
│   │   ├── types/              # Shared data types
│   │   └── ebpf/               # eBPF loader
│   └── internal/               # Private packages
│       ├── agent/
│       ├── collector/
│       ├── storage/
│       ├── engine/
│       ├── intelligence/
│       └── api/
├── frontend/                   # Next.js dashboard
├── deploy/                     # Deployment configs
│   ├── docker/
│   └── helm/
├── docs/                       # Documentation
├── go.mod                      # Go module
├── Makefile                    # Build automation
└── README.md
```

## 🚀 Quick Start

### Prerequisites

- Docker & Docker Compose
- Go 1.22+
- Node.js 20+
- kubectl (for Kubernetes deployment)

### Development Setup

```bash
# Clone the repository
git clone https://github.com/egressor/egressor
cd egressor

# Start development databases
make dev-db

# Build all binaries
make build

# Run the collector
./bin/egressor-collector --debug

# Run the API server (in another terminal)
./bin/egressor-api --debug

# Start the frontend (in another terminal)
cd frontend && npm install && npm run dev
```

### Docker Compose

```bash
# Start everything
docker-compose -f deploy/docker/docker-compose.dev.yml up -d

# Access the UI
open http://localhost:3000
```

### Kubernetes (Helm)

```bash
helm install egressor deploy/helm/egressor \
  --namespace egressor \
  --create-namespace
```

## 📊 API Endpoints

### Graph
- `GET /api/v1/graph` - Full transfer graph
- `GET /api/v1/graph/stats` - Graph statistics
- `GET /api/v1/graph/service/{service}` - Service subgraph
- `GET /api/v1/graph/top-talkers` - Top services by bytes sent

### Costs
- `GET /api/v1/costs/summary` - Cost summary
- `GET /api/v1/costs/attribution` - Cost attribution by service

### Anomalies
- `GET /api/v1/anomalies` - All anomalies
- `GET /api/v1/anomalies/active` - Active anomalies

### Intelligence (Claude)
- `POST /api/v1/intelligence/analyze` - General analysis
- `POST /api/v1/intelligence/ask` - Ask a question

## 🔧 Configuration

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `FLOWSCOPE_CLICKHOUSE_DSN` | ClickHouse connection string | `clickhouse://localhost:9000/egressor` |
| `FLOWSCOPE_POSTGRES_DSN` | PostgreSQL connection string | `postgres://localhost:5432/egressor` |
| `FLOWSCOPE_ANTHROPIC_API_KEY` | Anthropic API key for Claude | (none) |
| `FLOWSCOPE_DEBUG` | Enable debug logging | `false` |

## 🔐 Security

- **eBPF requires privileged access** - Agent runs with elevated permissions
- **No deep packet inspection** - Only metadata, never payload content
- **Claude receives summaries only** - Never raw traffic data

## 📜 License

Apache 2.0
