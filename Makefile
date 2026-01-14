# FlowScope Makefile

.PHONY: all build clean test lint proto ebpf agent collector api frontend intelligence docker help

# Variables
GO := go
PYTHON := python
GOOS ?= linux
GOARCH ?= amd64
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -ldflags "-X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME)"

# Directories
BIN_DIR := bin
SRC_DIR := src
CMD_DIR := $(SRC_DIR)/cmd
PKG_DIR := $(SRC_DIR)/pkg
INTERNAL_DIR := $(SRC_DIR)/internal
EBPF_DIR := $(SRC_DIR)/ebpf
INTELLIGENCE_DIR := $(SRC_DIR)/intelligence
FRONTEND_DIR := frontend
PROTO_DIR := api/proto

# Colors
GREEN := \033[0;32m
YELLOW := \033[0;33m
NC := \033[0m

all: build ## Build all components

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "$(GREEN)%-20s$(NC) %s\n", $$1, $$2}'

#
# Build targets (Go - Data Path)
#

build: agent collector api ## Build all Go binaries
	@echo "$(GREEN)All Go binaries built successfully$(NC)"

agent: ## Build the node agent
	@echo "Building agent..."
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build $(LDFLAGS) -o $(BIN_DIR)/egressor-agent ./$(CMD_DIR)/agent

collector: ## Build the collector service
	@echo "Building collector..."
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build $(LDFLAGS) -o $(BIN_DIR)/egressor-collector ./$(CMD_DIR)/collector

api: ## Build the API server
	@echo "Building API server..."
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build $(LDFLAGS) -o $(BIN_DIR)/egressor-api ./$(CMD_DIR)/api

#
# eBPF targets
#

ebpf: ## Compile eBPF programs
	@echo "Compiling eBPF programs..."
	@mkdir -p $(BIN_DIR)/ebpf
	clang -O2 -g -target bpf -c $(EBPF_DIR)/flow_tracker.c -o $(BIN_DIR)/ebpf/flow_tracker.o
	clang -O2 -g -target bpf -c $(EBPF_DIR)/egress_monitor.c -o $(BIN_DIR)/ebpf/egress_monitor.o

generate: ## Generate Go code from eBPF objects
	@echo "Generating eBPF Go bindings..."
	go generate ./$(PKG_DIR)/ebpf/...

#
# Proto targets
#

proto: ## Generate protobuf code
	@echo "Generating protobuf code..."
	protoc --go_out=. --go-grpc_out=. $(PROTO_DIR)/*.proto

#
# Python Intelligence Service targets
#

intelligence-install: ## Install Python intelligence service dependencies
	@echo "$(YELLOW)Installing Python intelligence service...$(NC)"
	cd $(INTELLIGENCE_DIR) && pip install -e .

intelligence-dev: ## Install Python intelligence with dev dependencies
	@echo "$(YELLOW)Installing Python intelligence service (dev mode)...$(NC)"
	cd $(INTELLIGENCE_DIR) && pip install -e ".[dev]"

intelligence-run: ## Run Python intelligence service
	@echo "$(YELLOW)Starting intelligence service...$(NC)"
	cd $(INTELLIGENCE_DIR) && egressor-intelligence

intelligence-lint: ## Lint Python intelligence service
	@echo "Running Python linters..."
	cd $(INTELLIGENCE_DIR) && ruff check .
	cd $(INTELLIGENCE_DIR) && mypy intelligence

intelligence-test: ## Test Python intelligence service
	@echo "Running Python tests..."
	cd $(INTELLIGENCE_DIR) && pytest

#
# Test targets
#

test: test-go test-python ## Run all tests

test-go: ## Run Go tests
	@echo "Running Go tests..."
	$(GO) test -v -race -coverprofile=coverage.out ./$(SRC_DIR)/...

test-python: ## Run Python tests
	@echo "Running Python tests..."
	cd $(INTELLIGENCE_DIR) && pytest -v

test-short: ## Run short tests only
	$(GO) test -v -short ./$(SRC_DIR)/...

coverage: test-go ## Generate Go coverage report
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

#
# Lint targets
#

lint: lint-go lint-python ## Run all linters

lint-go: ## Run Go linters
	@echo "Running Go linters..."
	golangci-lint run ./$(SRC_DIR)/...

lint-python: ## Run Python linters
	@echo "Running Python linters..."
	-cd $(INTELLIGENCE_DIR) && ruff check .

fmt: ## Format code
	$(GO) fmt ./$(SRC_DIR)/...
	gofumpt -l -w $(SRC_DIR)
	-cd $(INTELLIGENCE_DIR) && ruff format .

#
# Frontend targets
#

frontend: ## Build frontend
	@echo "Building frontend..."
	cd $(FRONTEND_DIR) && npm install && npm run build

frontend-dev: ## Run frontend in dev mode
	cd $(FRONTEND_DIR) && npm run dev

#
# Docker targets
#

docker: docker-agent docker-collector docker-api docker-intelligence docker-frontend ## Build all Docker images

docker-agent: ## Build agent Docker image
	docker build --build-arg SERVICE=agent -t egressor/agent:$(VERSION) -f $(SRC_DIR)/Dockerfile .

docker-collector: ## Build collector Docker image
	docker build --build-arg SERVICE=collector -t egressor/collector:$(VERSION) -f $(SRC_DIR)/Dockerfile .

docker-api: ## Build API Docker image
	docker build --build-arg SERVICE=api -t egressor/api:$(VERSION) -f $(SRC_DIR)/Dockerfile .

docker-intelligence: ## Build Python intelligence Docker image
	docker build -t egressor/intelligence:$(VERSION) $(INTELLIGENCE_DIR)

docker-frontend: ## Build frontend Docker image
	docker build -t egressor/frontend:$(VERSION) $(FRONTEND_DIR)

#
# Development targets
#

dev-setup: dev-setup-go dev-setup-python ## Set up full development environment

dev-setup-go: ## Set up Go development environment
	@echo "Setting up Go development environment..."
	$(GO) install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	$(GO) install mvdan.cc/gofumpt@latest
	$(GO) install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	$(GO) install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	$(GO) install github.com/cilium/ebpf/cmd/bpf2go@latest

dev-setup-python: ## Set up Python development environment
	@echo "Setting up Python development environment..."
	cd $(INTELLIGENCE_DIR) && pip install -e ".[dev]"

dev-db: ## Start development databases
	docker-compose -f deploy/docker-compose.dev.yml up -d clickhouse postgres redis

dev-stop: ## Stop development stack
	docker-compose -f deploy/docker-compose.dev.yml down

dev-up: ## Start full development stack
	docker-compose -f deploy/docker-compose.dev.yml up -d

dev-logs: ## View development logs
	docker-compose -f deploy/docker-compose.dev.yml logs -f

mock-data: ## Generate mock data once (100 flows + 1 anomaly)
	@echo "Generating mock data..."
	@curl -s -X POST "http://localhost:8080/api/v1/mock/generate?count=100" | jq .
	@curl -s -X POST "http://localhost:8080/api/v1/mock/anomaly" | jq .

mock-realtime: ## Start real-time mock data generator
	@./scripts/mock-realtime.sh

mock-reset: ## Reset all mock data
	@curl -s -X DELETE "http://localhost:8080/api/v1/mock/reset" | jq .

#
# Clean targets
#

clean: ## Clean build artifacts
	@echo "Cleaning..."
	rm -rf $(BIN_DIR)
	rm -f coverage.out coverage.html
	$(GO) clean -cache -testcache

#
# Deploy targets
#

deploy-helm: ## Deploy using Helm
	helm upgrade --install egressor deploy/helm/egressor -n egressor --create-namespace

deploy-local: build intelligence-install dev-db ## Deploy locally for testing
	@echo "Starting local deployment..."
	./$(BIN_DIR)/egressor-collector &
	./$(BIN_DIR)/egressor-api &
	cd $(INTELLIGENCE_DIR) && egressor-intelligence &
