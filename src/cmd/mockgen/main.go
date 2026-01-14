// Package main provides a mock data generator for testing Egressor.
package main

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"github.com/egressor/egressor/src/internal/collector"
	"github.com/egressor/egressor/src/pkg/types"
	"github.com/google/uuid"
)

var (
	services = []string{
		"api-gateway", "user-service", "order-service", "payment-service",
		"inventory-service", "notification-service", "analytics-service",
		"auth-service", "cache-service", "search-service",
	}

	namespaces = []string{"default", "production", "staging", "monitoring"}

	regions = []string{"us-east-1", "us-west-2", "eu-west-1", "ap-southeast-1"}

	azs = map[string][]string{
		"us-east-1":      {"us-east-1a", "us-east-1b", "us-east-1c"},
		"us-west-2":      {"us-west-2a", "us-west-2b", "us-west-2c"},
		"eu-west-1":      {"eu-west-1a", "eu-west-1b", "eu-west-1c"},
		"ap-southeast-1": {"ap-southeast-1a", "ap-southeast-1b", "ap-southeast-1c"},
	}

	externalDests = []string{
		"s3.amazonaws.com", "dynamodb.us-east-1.amazonaws.com",
		"api.stripe.com", "api.twilio.com", "hooks.slack.com",
		"api.datadog.com", "logs.us-east-1.amazonaws.com",
	}
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "egressor-mockgen",
		Short: "Generate mock data for Egressor testing",
		RunE:  run,
	}

	rootCmd.Flags().String("collector-url", "http://localhost:8081", "Collector HTTP URL")
	rootCmd.Flags().Duration("interval", 1*time.Second, "Generation interval")
	rootCmd.Flags().Int("events-per-tick", 50, "Events to generate per interval")
	rootCmd.Flags().Float64("anomaly-chance", 0.05, "Chance of generating anomaly (0-1)")
	rootCmd.Flags().Bool("debug", true, "Enable debug logging")

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func run(cmd *cobra.Command, args []string) error {
	debug, _ := cmd.Flags().GetBool("debug")
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	if debug {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	}

	collectorURL, _ := cmd.Flags().GetString("collector-url")
	interval, _ := cmd.Flags().GetDuration("interval")
	eventsPerTick, _ := cmd.Flags().GetInt("events-per-tick")
	anomalyChance, _ := cmd.Flags().GetFloat64("anomaly-chance")

	log.Info().
		Str("collector", collectorURL).
		Dur("interval", interval).
		Int("events_per_tick", eventsPerTick).
		Float64("anomaly_chance", anomalyChance).
		Msg("Starting mock data generator")

	gen := NewMockGenerator(collectorURL, eventsPerTick, anomalyChance)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start generation loop
	go gen.Run(ctx, interval)

	// Wait for shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Info().Msg("Shutting down mock generator...")
	cancel()
	return nil
}

// MockGenerator generates realistic mock transfer events.
type MockGenerator struct {
	collectorURL  string
	eventsPerTick int
	anomalyChance float64
	collector     *collector.Collector

	// Track service locations for consistent topology
	serviceLocations map[string]ServiceLocation
}

type ServiceLocation struct {
	Region    string
	AZ        string
	Namespace string
	PodIP     string
}

func NewMockGenerator(collectorURL string, eventsPerTick int, anomalyChance float64) *MockGenerator {
	g := &MockGenerator{
		collectorURL:     collectorURL,
		eventsPerTick:    eventsPerTick,
		anomalyChance:    anomalyChance,
		serviceLocations: make(map[string]ServiceLocation),
	}

	// Initialize service locations
	for _, svc := range services {
		region := regions[rand.Intn(len(regions))]
		g.serviceLocations[svc] = ServiceLocation{
			Region:    region,
			AZ:        azs[region][rand.Intn(len(azs[region]))],
			Namespace: namespaces[rand.Intn(len(namespaces))],
			PodIP:     fmt.Sprintf("10.%d.%d.%d", rand.Intn(255), rand.Intn(255), rand.Intn(255)),
		}
	}

	return g
}

func (g *MockGenerator) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	eventCount := 0
	for {
		select {
		case <-ctx.Done():
			log.Info().Int("total_events", eventCount).Msg("Generator stopped")
			return
		case <-ticker.C:
			events := g.generateEvents()
			eventCount += len(events)

			// Log stats
			var totalBytes uint64
			var egressBytes uint64
			for _, e := range events {
				totalBytes += e.BytesSent
				if e.IsEgress {
					egressBytes += e.BytesSent
				}
			}

			log.Debug().
				Int("events", len(events)).
				Uint64("total_bytes", totalBytes).
				Uint64("egress_bytes", egressBytes).
				Int("total_generated", eventCount).
				Msg("Generated events")
		}
	}
}

func (g *MockGenerator) generateEvents() []types.TransferEvent {
	events := make([]types.TransferEvent, 0, g.eventsPerTick)

	for i := 0; i < g.eventsPerTick; i++ {
		event := g.generateEvent()
		events = append(events, event)
	}

	return events
}

func (g *MockGenerator) generateEvent() types.TransferEvent {
	srcService := services[rand.Intn(len(services))]
	srcLoc := g.serviceLocations[srcService]

	// Decide traffic type
	trafficType := rand.Float64()

	var event types.TransferEvent
	event.ID = uuid.New()
	event.Timestamp = time.Now()
	event.NodeName = fmt.Sprintf("node-%s-%d", srcLoc.AZ, rand.Intn(10))
	event.Protocol = types.ProtocolTCP

	// Source info
	event.SourceIP = srcLoc.PodIP
	event.SourcePort = uint16(30000 + rand.Intn(30000))
	event.SourceService = srcService
	event.SourceNamespace = srcLoc.Namespace
	event.SourcePod = fmt.Sprintf("%s-%s", srcService, randomString(5))

	if trafficType < 0.6 {
		// Internal traffic (60%)
		dstService := services[rand.Intn(len(services))]
		dstLoc := g.serviceLocations[dstService]

		event.DestIP = dstLoc.PodIP
		event.DestPort = uint16(8080 + rand.Intn(10))
		event.DestService = dstService
		event.DestNamespace = dstLoc.Namespace
		event.DestPod = fmt.Sprintf("%s-%s", dstService, randomString(5))
		event.IsEgress = false
		event.IsCrossRegion = srcLoc.Region != dstLoc.Region
		event.IsCrossAZ = srcLoc.AZ != dstLoc.AZ

		// Internal traffic is usually smaller
		event.BytesSent = uint64(1024 + rand.Intn(100*1024))
		event.BytesReceived = uint64(1024 + rand.Intn(50*1024))

	} else if trafficType < 0.85 {
		// External API calls (25%)
		extDest := externalDests[rand.Intn(len(externalDests))]

		event.DestIP = fmt.Sprintf("52.%d.%d.%d", rand.Intn(255), rand.Intn(255), rand.Intn(255))
		event.DestPort = 443
		event.DestService = extDest
		event.IsEgress = true
		event.IsCrossRegion = true

		// API calls are medium sized
		event.BytesSent = uint64(512 + rand.Intn(50*1024))
		event.BytesReceived = uint64(1024 + rand.Intn(200*1024))

	} else {
		// Data transfer to S3/storage (15%)
		event.DestIP = fmt.Sprintf("52.%d.%d.%d", rand.Intn(255), rand.Intn(255), rand.Intn(255))
		event.DestPort = 443
		event.DestService = "s3.amazonaws.com"
		event.IsEgress = true
		event.IsCrossRegion = rand.Float64() < 0.3 // 30% cross-region

		// Data transfers are larger
		event.BytesSent = uint64(10*1024 + rand.Intn(10*1024*1024))
		event.BytesReceived = uint64(512 + rand.Intn(1024))
	}

	// Maybe generate anomaly
	if rand.Float64() < g.anomalyChance {
		// Spike the traffic
		event.BytesSent *= uint64(10 + rand.Intn(50))
		log.Info().
			Str("source", event.SourceService).
			Str("dest", event.DestService).
			Uint64("bytes", event.BytesSent).
			Msg("Generated anomalous traffic spike")
	}

	event.PacketCount = uint64(event.BytesSent/1460 + 1)
	event.Duration = time.Duration(100+rand.Intn(5000)) * time.Millisecond

	return event
}

func randomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

// HTTP client to send events to collector (future use)
var httpClient = &http.Client{Timeout: 5 * time.Second}
