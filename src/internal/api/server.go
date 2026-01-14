// Package api implements the FlowScope API server.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"

	"github.com/egressor/egressor/src/internal/engine"
	"github.com/egressor/egressor/src/internal/storage"
	"github.com/egressor/egressor/src/pkg/types"
)

// Config holds API server configuration.
type Config struct {
	HTTPListen      string
	GRPCListen      string
	ClickHouseDSN   string
	PostgresDSN     string
	IntelligenceURL string // URL to Python intelligence service
	CORSOrigins     []string
}

// Server is the FlowScope API server.
type Server struct {
	cfg             Config
	httpServer      *http.Server
	grpcServer      *grpc.Server
	storage         *storage.ClickHouseStore
	graphEngine     *engine.GraphEngine
	costEngine      *engine.CostEngine
	baseline        *engine.BaselineEngine
	intelligenceURL string
	httpClient      *http.Client
}

// NewServer creates a new API server.
func NewServer(cfg Config) (*Server, error) {
	// Initialize storage
	store, err := storage.NewClickHouseStore(cfg.ClickHouseDSN)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to connect to ClickHouse, some features disabled")
	}

	// Initialize engines
	graphEngine := engine.NewGraphEngine(store)
	costEngine := engine.NewCostEngine()
	baselineEngine := engine.NewBaselineEngine(3.0)

	// Default intelligence URL
	intelligenceURL := cfg.IntelligenceURL
	if intelligenceURL == "" {
		intelligenceURL = "http://localhost:8090"
	}

	return &Server{
		cfg:             cfg,
		storage:         store,
		graphEngine:     graphEngine,
		costEngine:      costEngine,
		baseline:        baselineEngine,
		intelligenceURL: intelligenceURL,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}, nil
}

// Start starts the API server.
func (s *Server) Start(ctx context.Context) error {
	// Set up HTTP router
	router := s.setupRouter()

	s.httpServer = &http.Server{
		Addr:         s.cfg.HTTPListen,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start HTTP server
	go func() {
		log.Info().Str("addr", s.cfg.HTTPListen).Msg("Starting HTTP server")
		if err := s.httpServer.ListenAndServe(); err != http.ErrServerClosed {
			log.Error().Err(err).Msg("HTTP server error")
		}
	}()

	// Start gRPC server
	if s.cfg.GRPCListen != "" {
		listener, err := net.Listen("tcp", s.cfg.GRPCListen)
		if err != nil {
			return fmt.Errorf("listening on gRPC address: %w", err)
		}

		s.grpcServer = grpc.NewServer()
		// Register gRPC services here

		go func() {
			log.Info().Str("addr", s.cfg.GRPCListen).Msg("Starting gRPC server")
			if err := s.grpcServer.Serve(listener); err != nil {
				log.Error().Err(err).Msg("gRPC server error")
			}
		}()
	}

	// Load initial data
	go s.loadInitialData(ctx)

	return nil
}

// Stop stops the API server.
func (s *Server) Stop(ctx context.Context) error {
	if s.httpServer != nil {
		if err := s.httpServer.Shutdown(ctx); err != nil {
			return fmt.Errorf("shutting down HTTP server: %w", err)
		}
	}

	if s.grpcServer != nil {
		s.grpcServer.GracefulStop()
	}

	if s.storage != nil {
		s.storage.Close()
	}

	return nil
}

// setupRouter configures the HTTP router.
func (s *Server) setupRouter() *chi.Mux {
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))

	// CORS
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   s.cfg.CORSOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Request-ID"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Health endpoints
	r.Get("/health", s.healthHandler)
	r.Get("/ready", s.readyHandler)
	r.Handle("/metrics", promhttp.Handler())

	// API v1 routes
	r.Route("/api/v1", func(r chi.Router) {
		// Graph endpoints
		r.Get("/graph", s.getGraph)
		r.Get("/graph/stats", s.getGraphStats)
		r.Get("/graph/service/{service}", s.getServiceGraph)
		r.Get("/graph/top-talkers", s.getTopTalkers)
		r.Get("/graph/top-edges", s.getTopEdges)

		// Flow endpoints
		r.Get("/flows", s.getFlows)
		r.Get("/flows/egress", s.getEgressFlows)
		r.Get("/flows/cross-region", s.getCrossRegionFlows)

		// Cost endpoints
		r.Get("/costs/summary", s.getCostSummary)
		r.Get("/costs/attribution", s.getCostAttribution)
		r.Get("/costs/by-namespace", s.getCostByNamespace)
		r.Get("/costs/by-service", s.getCostByService)

		// Anomaly endpoints
		r.Get("/anomalies", s.getAnomalies)
		r.Get("/anomalies/active", s.getActiveAnomalies)
		r.Get("/anomalies/{id}", s.getAnomaly)
		r.Get("/anomalies/summary", s.getAnomalySummary)
		r.Post("/anomalies/{id}/acknowledge", s.acknowledgeAnomaly)
		r.Post("/anomalies/{id}/resolve", s.resolveAnomaly)

		// Baseline endpoints
		r.Get("/baselines", s.getBaselines)
		r.Get("/baselines/{flowKey}", s.getBaseline)

		// Intelligence endpoints (proxied to Python service)
		r.Post("/intelligence/analyze", s.proxyToIntelligence)
		r.Post("/intelligence/investigate", s.proxyToIntelligence)
		r.Post("/intelligence/explain-cost", s.proxyToIntelligence)
		r.Post("/intelligence/ask", s.proxyToIntelligence)
		r.Get("/intelligence/optimizations", s.proxyToIntelligence)

		// Mock data endpoints (for testing)
		r.Post("/mock/generate", s.generateMockData)
		r.Post("/mock/anomaly", s.generateMockAnomaly)
		r.Delete("/mock/reset", s.resetMockData)
	})

	return r
}

// loadInitialData loads data from storage on startup.
func (s *Server) loadInitialData(ctx context.Context) {
	if s.storage == nil {
		return
	}

	// Load last 24 hours of data
	end := time.Now()
	start := end.Add(-24 * time.Hour)

	if err := s.graphEngine.LoadFromStorage(ctx, start, end); err != nil {
		log.Error().Err(err).Msg("Failed to load graph data")
	}
}

// Handler implementations

func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func (s *Server) readyHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Ready"))
}

func (s *Server) getGraph(w http.ResponseWriter, r *http.Request) {
	graph := s.graphEngine.GetGraph().ToJSON()
	s.jsonResponse(w, http.StatusOK, graph)
}

func (s *Server) getGraphStats(w http.ResponseWriter, r *http.Request) {
	stats := s.graphEngine.GetGraph().GetStats()
	s.jsonResponse(w, http.StatusOK, stats)
}

func (s *Server) getServiceGraph(w http.ResponseWriter, r *http.Request) {
	service := chi.URLParam(r, "service")
	depth := 2
	if d := r.URL.Query().Get("depth"); d != "" {
		if parsed, err := strconv.Atoi(d); err == nil {
			depth = parsed
		}
	}

	subgraph := s.graphEngine.GetGraph().GetServiceGraph(service, depth)
	s.jsonResponse(w, http.StatusOK, subgraph.ToJSON())
}

func (s *Server) getTopTalkers(w http.ResponseWriter, r *http.Request) {
	n := 10
	if nStr := r.URL.Query().Get("n"); nStr != "" {
		if parsed, err := strconv.Atoi(nStr); err == nil {
			n = parsed
		}
	}

	talkers := s.graphEngine.GetGraph().GetTopTalkers(n)
	nodes := make([]engine.NodeJSON, len(talkers))
	for i, t := range talkers {
		nodes[i] = engine.NodeJSON{
			ID:                 t.ID,
			Namespace:          t.Namespace,
			Name:               t.Name,
			TotalBytesSent:     t.TotalBytesSent,
			TotalBytesReceived: t.TotalBytesReceived,
			TotalConnections:   t.TotalConnections,
		}
	}
	s.jsonResponse(w, http.StatusOK, nodes)
}

func (s *Server) getTopEdges(w http.ResponseWriter, r *http.Request) {
	n := 10
	if nStr := r.URL.Query().Get("n"); nStr != "" {
		if parsed, err := strconv.Atoi(nStr); err == nil {
			n = parsed
		}
	}

	edges := s.graphEngine.GetGraph().GetTopEdges(n)
	result := make([]engine.EdgeJSON, len(edges))
	for i, e := range edges {
		result[i] = engine.EdgeJSON{
			Source:       e.SourceID,
			Target:       e.DestinationID,
			TransferType: string(e.TransferType),
			TotalBytes:   e.TotalBytes,
			TotalEvents:  e.TotalEvents,
			CostUSD:      e.TotalCostUSD,
		}
	}
	s.jsonResponse(w, http.StatusOK, result)
}

func (s *Server) getFlows(w http.ResponseWriter, r *http.Request) {
	if s.storage == nil {
		s.jsonResponse(w, http.StatusOK, []interface{}{})
		return
	}

	start := time.Now().Add(-24 * time.Hour)
	end := time.Now()

	flows, err := s.storage.QueryFlows(r.Context(), storage.FlowQuery{
		Start: start,
		End:   end,
		Limit: 100,
	})
	if err != nil {
		s.errorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.jsonResponse(w, http.StatusOK, flows)
}

func (s *Server) getEgressFlows(w http.ResponseWriter, r *http.Request) {
	edges := s.graphEngine.GetGraph().GetEgressEdges()
	result := make([]engine.EdgeJSON, len(edges))
	for i, e := range edges {
		result[i] = engine.EdgeJSON{
			Source:       e.SourceID,
			Target:       e.DestinationID,
			TransferType: string(e.TransferType),
			TotalBytes:   e.TotalBytes,
			TotalEvents:  e.TotalEvents,
			CostUSD:      e.TotalCostUSD,
		}
	}
	s.jsonResponse(w, http.StatusOK, result)
}

func (s *Server) getCrossRegionFlows(w http.ResponseWriter, r *http.Request) {
	edges := s.graphEngine.GetGraph().GetCrossRegionEdges()
	result := make([]engine.EdgeJSON, len(edges))
	for i, e := range edges {
		result[i] = engine.EdgeJSON{
			Source:       e.SourceID,
			Target:       e.DestinationID,
			TransferType: string(e.TransferType),
			TotalBytes:   e.TotalBytes,
			TotalEvents:  e.TotalEvents,
			CostUSD:      e.TotalCostUSD,
		}
	}
	s.jsonResponse(w, http.StatusOK, result)
}

func (s *Server) getCostSummary(w http.ResponseWriter, r *http.Request) {
	summary := map[string]interface{}{
		"total_cost_usd":        125.50,
		"egress_cost_usd":       80.25,
		"cross_region_cost_usd": 30.00,
		"cross_az_cost_usd":     15.25,
		"total_bytes":           1500000000000,
		"by_namespace":          map[string]float64{},
		"by_service":            map[string]float64{},
	}
	s.jsonResponse(w, http.StatusOK, summary)
}

func (s *Server) getCostAttribution(w http.ResponseWriter, r *http.Request) {
	s.jsonResponse(w, http.StatusOK, []interface{}{})
}

func (s *Server) getCostByNamespace(w http.ResponseWriter, r *http.Request) {
	s.jsonResponse(w, http.StatusOK, map[string]float64{})
}

func (s *Server) getCostByService(w http.ResponseWriter, r *http.Request) {
	s.jsonResponse(w, http.StatusOK, map[string]float64{})
}

func (s *Server) getAnomalies(w http.ResponseWriter, r *http.Request) {
	anomalies := s.baseline.GetActiveAnomalies()
	if anomalies == nil {
		anomalies = []*types.Anomaly{}
	}
	s.jsonResponse(w, http.StatusOK, anomalies)
}

func (s *Server) getActiveAnomalies(w http.ResponseWriter, r *http.Request) {
	anomalies := s.baseline.GetActiveAnomalies()
	if anomalies == nil {
		anomalies = []*types.Anomaly{}
	}
	s.jsonResponse(w, http.StatusOK, anomalies)
}

func (s *Server) getAnomaly(w http.ResponseWriter, r *http.Request) {
	// Return specific anomaly by ID
	s.errorResponse(w, http.StatusNotFound, "anomaly not found")
}

func (s *Server) getAnomalySummary(w http.ResponseWriter, r *http.Request) {
	summary := s.baseline.GetAnomalySummary()
	s.jsonResponse(w, http.StatusOK, summary)
}

func (s *Server) acknowledgeAnomaly(w http.ResponseWriter, r *http.Request) {
	s.jsonResponse(w, http.StatusOK, map[string]string{"status": "acknowledged"})
}

func (s *Server) resolveAnomaly(w http.ResponseWriter, r *http.Request) {
	s.jsonResponse(w, http.StatusOK, map[string]string{"status": "resolved"})
}

func (s *Server) getBaselines(w http.ResponseWriter, r *http.Request) {
	baselines := s.baseline.GetAllBaselines()
	s.jsonResponse(w, http.StatusOK, baselines)
}

func (s *Server) getBaseline(w http.ResponseWriter, r *http.Request) {
	flowKey := chi.URLParam(r, "flowKey")
	baseline := s.baseline.GetBaseline(flowKey)
	if baseline == nil {
		s.errorResponse(w, http.StatusNotFound, "baseline not found")
		return
	}
	s.jsonResponse(w, http.StatusOK, baseline)
}

// proxyToIntelligence proxies requests to the Python intelligence service.
func (s *Server) proxyToIntelligence(w http.ResponseWriter, r *http.Request) {
	// Build the target URL
	path := r.URL.Path
	// Remove /api/v1/intelligence prefix and map to Python service endpoints
	var targetPath string
	switch {
	case path == "/api/v1/intelligence/analyze":
		targetPath = "/analyze"
	case path == "/api/v1/intelligence/investigate":
		targetPath = "/investigate"
	case path == "/api/v1/intelligence/explain-cost":
		targetPath = "/explain-cost"
	case path == "/api/v1/intelligence/ask":
		targetPath = "/ask"
	case path == "/api/v1/intelligence/optimizations":
		targetPath = "/optimizations"
	default:
		s.errorResponse(w, http.StatusNotFound, "unknown intelligence endpoint")
		return
	}

	targetURL := s.intelligenceURL + targetPath

	// Create proxy request
	proxyReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, r.Body)
	if err != nil {
		s.errorResponse(w, http.StatusInternalServerError, "failed to create proxy request")
		return
	}

	// Copy headers
	proxyReq.Header = r.Header.Clone()

	// Make request
	resp, err := s.httpClient.Do(proxyReq)
	if err != nil {
		log.Error().Err(err).Str("url", targetURL).Msg("Intelligence service unavailable")
		s.errorResponse(w, http.StatusServiceUnavailable, "intelligence service unavailable")
		return
	}
	defer resp.Body.Close()

	// Copy response headers
	for k, v := range resp.Header {
		w.Header()[k] = v
	}

	// Copy status and body
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// Mock data handlers

var mockServices = []string{
	"api-gateway", "user-service", "order-service", "payment-service",
	"inventory-service", "notification-service", "analytics-service",
	"auth-service", "cache-service", "search-service",
}

var mockNamespaces = []string{"default", "production", "staging", "monitoring"}
var mockRegions = []string{"us-east-1", "us-west-2", "eu-west-1", "ap-southeast-1"}
var mockExternalDests = []string{
	"s3.amazonaws.com", "dynamodb.us-east-1.amazonaws.com",
	"api.stripe.com", "api.twilio.com", "hooks.slack.com",
}

func (s *Server) generateMockData(w http.ResponseWriter, r *http.Request) {
	countStr := r.URL.Query().Get("count")
	count := 100
	if countStr != "" {
		if c, err := strconv.Atoi(countStr); err == nil && c > 0 && c <= 10000 {
			count = c
		}
	}

	flows := make([]types.TransferFlow, 0, count)
	var totalBytes, egressBytes, crossRegionBytes uint64

	for i := 0; i < count; i++ {
		flow := generateMockFlow()
		flows = append(flows, flow)

		// Update graph engine
		s.graphEngine.AddFlow(flow)

		totalBytes += flow.TotalBytes
		if flow.Type == types.TransferTypeEgress {
			egressBytes += flow.TotalBytes
		}
		if flow.Type == types.TransferTypeCrossRegion {
			crossRegionBytes += flow.TotalBytes
		}
	}

	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"generated":          count,
		"total_bytes":        totalBytes,
		"egress_bytes":       egressBytes,
		"cross_region_bytes": crossRegionBytes,
	})
}

func (s *Server) generateMockAnomaly(w http.ResponseWriter, r *http.Request) {
	srcService := mockServices[rand.Intn(len(mockServices))]
	dstService := mockExternalDests[rand.Intn(len(mockExternalDests))]
	spikeBytes := uint64(50*1024*1024 + rand.Intn(100*1024*1024)) // 50-150 MB spike

	// Create anomaly
	now := time.Now()
	anomaly := &types.Anomaly{
		ID:                        uuid.New(),
		Type:                      types.AnomalyTypeSpike,
		Severity:                  types.SeverityHigh,
		SourceService:             srcService,
		DestinationEndpoint:       dstService,
		DetectedAt:                now,
		StartedAt:                 &now,
		CurrentValue:              float64(spikeBytes),
		BaselineValue:             float64(spikeBytes) / 30,
		Deviation:                 29.0,
		AbsoluteDelta:             float64(spikeBytes) * 0.97,
		EstimatedCostImpactUSD:    float64(spikeBytes) / (1024 * 1024 * 1024) * 0.09,
		EstimatedMonthlyImpactUSD: float64(spikeBytes) / (1024 * 1024 * 1024) * 0.09 * 30,
		PotentialCauses:           []string{"Bulk data export", "Log shipping spike", "Backup job"},
		SuggestedActions:          []string{"Review data export jobs", "Check scheduled tasks", "Verify backup configurations"},
		CreatedAt:                 now,
		UpdatedAt:                 now,
	}

	s.baseline.AddAnomaly(anomaly)

	// Also add a corresponding flow
	flow := types.TransferFlow{
		ID:             uuid.New(),
		SourceIdentity: types.ServiceIdentity{Namespace: "production", Name: srcService},
		DestinationEndpoint: &types.Endpoint{
			Type:     types.EndpointTypeExternal,
			IP:       fmt.Sprintf("52.%d.%d.%d", rand.Intn(255), rand.Intn(255), rand.Intn(255)),
			Port:     443,
			Hostname: dstService,
		},
		Type:        types.TransferTypeEgress,
		TotalBytes:  spikeBytes,
		EventCount:  uint64(1000 + rand.Intn(5000)),
		WindowStart: now.Add(-1 * time.Hour),
		WindowEnd:   now,
	}
	s.graphEngine.AddFlow(flow)

	s.jsonResponse(w, http.StatusOK, anomaly)
}

func (s *Server) resetMockData(w http.ResponseWriter, r *http.Request) {
	// Reset engines
	s.graphEngine = engine.NewGraphEngine(s.storage)
	s.baseline = engine.NewBaselineEngine(3.0)
	s.costEngine = engine.NewCostEngine()

	s.jsonResponse(w, http.StatusOK, map[string]string{"status": "reset"})
}

func generateMockFlow() types.TransferFlow {
	srcService := mockServices[rand.Intn(len(mockServices))]
	srcNamespace := mockNamespaces[rand.Intn(len(mockNamespaces))]
	srcRegion := mockRegions[rand.Intn(len(mockRegions))]

	now := time.Now()
	windowStart := now.Add(-time.Duration(rand.Intn(60)) * time.Minute)

	flow := types.TransferFlow{
		ID: uuid.New(),
		SourceIdentity: types.ServiceIdentity{
			Namespace:        srcNamespace,
			Name:             srcService,
			Kind:             "Deployment",
			Region:           srcRegion,
			AvailabilityZone: srcRegion + string('a'+rune(rand.Intn(3))),
		},
		WindowStart: windowStart,
		WindowEnd:   now,
		EventCount:  uint64(10 + rand.Intn(500)),
	}

	trafficType := rand.Float64()

	if trafficType < 0.6 {
		// Internal traffic (service to service)
		dstService := mockServices[rand.Intn(len(mockServices))]
		dstNamespace := mockNamespaces[rand.Intn(len(mockNamespaces))]
		dstRegion := mockRegions[rand.Intn(len(mockRegions))]

		flow.DestinationIdentity = &types.ServiceIdentity{
			Namespace:        dstNamespace,
			Name:             dstService,
			Kind:             "Deployment",
			Region:           dstRegion,
			AvailabilityZone: dstRegion + string('a'+rune(rand.Intn(3))),
		}

		if srcRegion != dstRegion {
			flow.Type = types.TransferTypeCrossRegion
		} else {
			flow.Type = types.TransferTypeServiceToService
		}

		flow.TotalBytes = uint64(1024 + rand.Intn(100*1024))
		flow.TotalPackets = flow.TotalBytes / 1460

	} else if trafficType < 0.85 {
		// External API calls
		extDest := mockExternalDests[rand.Intn(len(mockExternalDests))]
		flow.DestinationEndpoint = &types.Endpoint{
			Type:           types.EndpointTypeExternal,
			IP:             fmt.Sprintf("52.%d.%d.%d", rand.Intn(255), rand.Intn(255), rand.Intn(255)),
			Port:           443,
			Hostname:       extDest,
			IsInternet:     true,
			IsCloudService: true,
		}
		flow.Type = types.TransferTypeEgress
		flow.TotalBytes = uint64(512 + rand.Intn(50*1024))
		flow.TotalPackets = flow.TotalBytes / 1460

	} else {
		// S3 data transfer (larger)
		flow.DestinationEndpoint = &types.Endpoint{
			Type:             types.EndpointTypeExternal,
			IP:               fmt.Sprintf("52.%d.%d.%d", rand.Intn(255), rand.Intn(255), rand.Intn(255)),
			Port:             443,
			Hostname:         "s3.amazonaws.com",
			IsInternet:       true,
			IsCloudService:   true,
			CloudServiceName: "S3",
		}
		flow.Type = types.TransferTypeEgress
		flow.TotalBytes = uint64(1*1024*1024 + rand.Intn(10*1024*1024))
		flow.TotalPackets = flow.TotalBytes / 1460
	}

	// Calculate rate stats
	durationSec := flow.WindowEnd.Sub(flow.WindowStart).Seconds()
	if durationSec > 0 {
		flow.BytesPerSecondAvg = float64(flow.TotalBytes) / durationSec
		flow.BytesPerSecondMax = flow.BytesPerSecondAvg * (1.0 + rand.Float64())
	}

	return flow
}

func randomMockString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

// Response helpers

func (s *Server) jsonResponse(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func (s *Server) errorResponse(w http.ResponseWriter, status int, message string) {
	s.jsonResponse(w, status, map[string]string{"error": message})
}
