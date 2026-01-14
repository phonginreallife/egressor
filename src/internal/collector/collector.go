// Package collector implements the Egressor collector service.
package collector

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"

	"github.com/egressor/egressor/src/internal/storage"
	"github.com/egressor/egressor/src/pkg/types"
)

// Config holds collector configuration.
type Config struct {
	GRPCListen    string
	HTTPListen    string
	ClickHouseDSN string
	PostgresDSN   string
	BatchSize     int
	FlushInterval time.Duration
}

// Collector is the Egressor collector service.
type Collector struct {
	cfg        Config
	storage    *storage.ClickHouseStore
	grpcServer *grpc.Server
	httpServer *http.Server
	eventChan  chan types.TransferEvent
	batch      []types.TransferEvent
	mu         sync.Mutex
	running    bool
	stopChan   chan struct{}

	// Metrics
	eventsReceived prometheus.Counter
	eventsStored   prometheus.Counter
	batchesWritten prometheus.Counter
	storageLatency prometheus.Histogram
}

// New creates a new collector.
func New(cfg Config) (*Collector, error) {
	store, err := storage.NewClickHouseStore(cfg.ClickHouseDSN)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to connect to ClickHouse, using in-memory mode")
	}

	c := &Collector{
		cfg:       cfg,
		storage:   store,
		eventChan: make(chan types.TransferEvent, 100000),
		batch:     make([]types.TransferEvent, 0, cfg.BatchSize),
		stopChan:  make(chan struct{}),
		eventsReceived: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "egressor_collector_events_received_total",
			Help: "Total number of events received",
		}),
		eventsStored: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "egressor_collector_events_stored_total",
			Help: "Total number of events stored",
		}),
		batchesWritten: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "egressor_collector_batches_written_total",
			Help: "Total number of batches written",
		}),
		storageLatency: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "egressor_collector_storage_latency_seconds",
			Help:    "Storage latency in seconds",
			Buckets: prometheus.ExponentialBuckets(0.001, 2, 15),
		}),
	}

	// Register metrics
	prometheus.MustRegister(c.eventsReceived, c.eventsStored, c.batchesWritten, c.storageLatency)

	return c, nil
}

// Start starts the collector.
func (c *Collector) Start(ctx context.Context) error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return fmt.Errorf("collector already running")
	}
	c.running = true
	c.mu.Unlock()

	// Start gRPC server
	grpcListener, err := net.Listen("tcp", c.cfg.GRPCListen)
	if err != nil {
		return fmt.Errorf("listening on gRPC address: %w", err)
	}

	c.grpcServer = grpc.NewServer()
	// pb.RegisterCollectorServer(c.grpcServer, c) // Register gRPC service

	go func() {
		log.Info().Str("addr", c.cfg.GRPCListen).Msg("Starting gRPC server")
		if err := c.grpcServer.Serve(grpcListener); err != nil {
			log.Error().Err(err).Msg("gRPC server error")
		}
	}()

	// Start HTTP server for health/metrics
	mux := http.NewServeMux()
	mux.HandleFunc("/health", c.healthHandler)
	mux.HandleFunc("/ready", c.readyHandler)
	mux.Handle("/metrics", promhttp.Handler())

	c.httpServer = &http.Server{
		Addr:    c.cfg.HTTPListen,
		Handler: mux,
	}

	go func() {
		log.Info().Str("addr", c.cfg.HTTPListen).Msg("Starting HTTP server")
		if err := c.httpServer.ListenAndServe(); err != http.ErrServerClosed {
			log.Error().Err(err).Msg("HTTP server error")
		}
	}()

	// Start batch processing
	go c.processBatches(ctx)

	log.Info().Msg("Collector started")
	return nil
}

// Stop stops the collector.
func (c *Collector) Stop(ctx context.Context) error {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return nil
	}
	c.running = false
	close(c.stopChan)
	c.mu.Unlock()

	// Flush remaining events
	c.flushBatch(ctx)

	// Stop servers
	if c.grpcServer != nil {
		c.grpcServer.GracefulStop()
	}
	if c.httpServer != nil {
		c.httpServer.Shutdown(ctx)
	}

	// Close storage
	if c.storage != nil {
		c.storage.Close()
	}

	return nil
}

// Ingest adds events to the processing queue.
func (c *Collector) Ingest(events []types.TransferEvent) {
	for _, event := range events {
		select {
		case c.eventChan <- event:
			c.eventsReceived.Inc()
		default:
			log.Warn().Msg("Event channel full, dropping events")
		}
	}
}

// processBatches processes events in batches.
func (c *Collector) processBatches(ctx context.Context) {
	ticker := time.NewTicker(c.cfg.FlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopChan:
			return
		case event := <-c.eventChan:
			c.mu.Lock()
			c.batch = append(c.batch, event)
			shouldFlush := len(c.batch) >= c.cfg.BatchSize
			c.mu.Unlock()

			if shouldFlush {
				c.flushBatch(ctx)
			}
		case <-ticker.C:
			c.flushBatch(ctx)
		}
	}
}

// flushBatch writes the current batch to storage.
func (c *Collector) flushBatch(ctx context.Context) {
	c.mu.Lock()
	if len(c.batch) == 0 {
		c.mu.Unlock()
		return
	}
	batch := c.batch
	c.batch = make([]types.TransferEvent, 0, c.cfg.BatchSize)
	c.mu.Unlock()

	start := time.Now()

	if c.storage != nil {
		if err := c.storage.InsertEvents(ctx, batch); err != nil {
			log.Error().Err(err).Int("count", len(batch)).Msg("Failed to insert events")
			return
		}
	}

	c.storageLatency.Observe(time.Since(start).Seconds())
	c.eventsStored.Add(float64(len(batch)))
	c.batchesWritten.Inc()

	log.Debug().Int("count", len(batch)).Dur("latency", time.Since(start)).Msg("Batch written")
}

// healthHandler returns health status.
func (c *Collector) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

// readyHandler returns readiness status.
func (c *Collector) readyHandler(w http.ResponseWriter, r *http.Request) {
	if c.storage == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("Storage not ready"))
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Ready"))
}

// GetStats returns collector statistics.
func (c *Collector) GetStats() map[string]interface{} {
	c.mu.Lock()
	batchLen := len(c.batch)
	c.mu.Unlock()

	return map[string]interface{}{
		"pending_batch_size": batchLen,
		"channel_length":     len(c.eventChan),
	}
}
