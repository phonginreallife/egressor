# Project: Egressor

## Purpose
Egressor detects, explains, and reduces unnecessary or expensive data transfer in Kubernetes and cloud systems.
It provides engineers with clear answers to:
- where data is moving
- why it is moving
- who caused it
- what can be done to reduce cost and risk

## Non-goals
- Not a generic observability platform
- Not a replacement for metrics/logs/traces
- Not a pure dashboarding product

## Primary Users
SREs, platform engineers, and infrastructure teams operating Kubernetes in cloud environments.

## Target Environment
Kubernetes-first (EKS initially), cloud-agnostic design.

## Key Problems Being Solved
- Unexpected cloud egress costs
- Hidden service-to-service data movement
- Difficult root cause analysis for traffic spikes
- No clear attribution of cost to workloads or deployments

## Architecture Summary
Agents capture data transfer signals at runtime using eBPF.
A collector aggregates flows into structured transfer events.
A transfer graph engine builds behavioral models.
A cost engine attributes traffic to cloud pricing.
Claude acts as the reasoning layer for investigation and optimization.

## Technology Stack

### Data Path (High Performance - Go)
- **Traffic Collection**: eBPF (C) + Go loader
- **Agent/Node Daemon**: Go
- **Transport**: OpenTelemetry + gRPC
- **Collector/Ingestion**: Go
- **Data Engine**: ClickHouse (analytics) + PostgreSQL (metadata)
- **Transfer Graph & Analytics**: Go
- **Cost Attribution**: Go
- **API/Backend**: Go + REST + gRPC (chi router)

### Intelligence Layer (Python)
- **Claude Integration**: Python + anthropic SDK
- **Investigation Workflows**: Python + FastAPI
- **Summary & Analysis**: Python
- **Non-realtime, human-facing, low-throughput operations**

### Frontend
- **Dashboard**: Next.js (TypeScript) + Tailwind CSS

## Why Python for Intelligence?
The intelligence/reasoning layer is a perfect fit for Python because it is:
- Low throughput (not handling raw traffic data)
- Human-facing (preparing reports, summaries)
- Non-realtime (no strict latency requirements)
- Non-critical for correctness (investigation, not billing)

Python shines here while Go handles the high-performance data path.

## Constraints
- Must handle high-volume traffic with minimal overhead
- Must preserve privacy and security of payloads
- No deep packet inspection of content, only metadata
- Prefer open standards (OTel, eBPF, Prometheus, etc.)

## Design Principles
- Correctness > performance > features
- Explainability is mandatory
- Every insight must be actionable
- Avoid magic; prefer deterministic models with AI on top

## Coding Rules
- Strong typing everywhere possible
- Deterministic processing for all billing-related logic
- All cost calculations must be reproducible
- Changes to data model require migration and versioning
- Go: follow effective Go, standard project layout
- Python: use type hints, pydantic models, async/await

## How to Work in This Repo
- All backend Go code lives in `/src/`
- eBPF programs (C) go in `/src/ebpf/`
- Application entrypoints go in `/src/cmd/{agent,collector,api}/`
- Public Go packages go in `/src/pkg/`
- Private Go packages go in `/src/internal/`
- **Python intelligence service lives in `/src/intelligence/`**
- Frontend code goes in `/frontend/`
- Deployment configs go in `/deploy/`
- All architectural decisions documented in `/docs/adr/`

## Repo Structure
```
/src                    # Backend
  Dockerfile            # Single Dockerfile for all Go services (use --build-arg SERVICE=...)
  /cmd                  # Go entrypoints (Go files only)
    /agent/main.go
    /collector/main.go
    /api/main.go
  /ebpf                 # eBPF programs (C)
    flow_tracker.c
    egress_monitor.c
  /pkg                  # Public Go packages
    /types
    /ebpf
  /internal             # Private Go packages
    /agent
    /collector
    /storage
    /engine
    /api
  /intelligence         # Python intelligence service
    Dockerfile
    pyproject.toml
    /intelligence
      __init__.py
      main.py
      api.py
      claude.py
      ...
/frontend               # Frontend (Next.js)
  Dockerfile
  package.json
  ...
/deploy                 # Deployment configs (docker-compose only)
  docker-compose.dev.yml
  /helm
/docs
/tests
/scripts
go.mod
Makefile
CLAUDE.md
README.md
```

## Build Commands
```bash
# Go (data path)
make build          # Build all Go binaries
make agent          # Build agent only
make collector      # Build collector only
make api            # Build API server only
make ebpf           # Compile eBPF programs
make test           # Run Go tests
make lint           # Run Go linters

# Python (intelligence)
cd src/intelligence && pip install -e .
cd src/intelligence && egressor-intelligence

# Docker
make docker         # Build Docker images
make dev-db         # Start development databases

# Full stack
docker-compose -f deploy/docker-compose.dev.yml up
```

## Service Architecture
```
┌─────────────────────────────────────────────────────────────────┐
│                        Frontend (Next.js)                        │
└───────────────────────────────┬─────────────────────────────────┘
                                │
        ┌───────────────────────┴───────────────────────┐
        │                                               │
        ▼                                               ▼
┌───────────────────┐                     ┌───────────────────────┐
│   Go API Server   │───── proxy ────────▶│  Python Intelligence  │
│   (Port 8080)     │                     │     (Port 8090)       │
└─────────┬─────────┘                     │                       │
          │                               │  - Claude API calls   │
          │                               │  - Investigation      │
          │                               │  - Summaries          │
          │                               │  - Optimizations      │
          │                               └───────────────────────┘
          │
          ▼
┌─────────────────────────────────────────────────────────────────┐
│                    Go Data Processing                            │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐        │
│  │ Collector│  │ Graph    │  │ Cost     │  │ Baseline │        │
│  │          │  │ Engine   │  │ Engine   │  │ Engine   │        │
│  └────┬─────┘  └────┬─────┘  └────┬─────┘  └────┬─────┘        │
└───────┼─────────────┼─────────────┼─────────────┼───────────────┘
        │             │             │             │
        ▼             ▼             ▼             ▼
┌─────────────────────────────────────────────────────────────────┐
│                    ClickHouse + PostgreSQL                       │
└─────────────────────────────────────────────────────────────────┘
```
