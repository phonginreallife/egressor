// Egressor Collector - Event ingestion and storage service
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/egressor/egressor/src/internal/collector"
)

var (
	Version   = "dev"
	BuildTime = "unknown"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "egressor-collector",
		Short: "Egressor Collector - Event ingestion and storage service",
		Long: `Egressor Collector receives transfer events from agents,
normalizes them, and stores them in ClickHouse for analysis.`,
		RunE: run,
	}

	// Flags
	rootCmd.Flags().String("config", "", "Config file path")
	rootCmd.Flags().String("grpc-listen", ":4317", "gRPC listen address")
	rootCmd.Flags().String("http-listen", ":8080", "HTTP listen address (health/metrics)")
	rootCmd.Flags().String("clickhouse-dsn", "clickhouse://localhost:9000/egressor", "ClickHouse DSN")
	rootCmd.Flags().String("postgres-dsn", "postgres://localhost:5432/egressor", "PostgreSQL DSN")
	rootCmd.Flags().Int("batch-size", 10000, "Batch size for ClickHouse inserts")
	rootCmd.Flags().Duration("flush-interval", 5*time.Second, "Flush interval for batches")
	rootCmd.Flags().Bool("debug", false, "Enable debug logging")

	viper.BindPFlags(rootCmd.Flags())
	viper.SetEnvPrefix("EGRESSOR")
	viper.AutomaticEnv()

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func run(cmd *cobra.Command, args []string) error {
	// Setup logging
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	if viper.GetBool("debug") {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	} else {
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}

	log.Info().
		Str("version", Version).
		Str("build_time", BuildTime).
		Msg("Starting Egressor Collector")

	// Load config
	if configFile := viper.GetString("config"); configFile != "" {
		viper.SetConfigFile(configFile)
		if err := viper.ReadInConfig(); err != nil {
			return fmt.Errorf("reading config: %w", err)
		}
	}

	cfg := collector.Config{
		GRPCListen:    viper.GetString("grpc-listen"),
		HTTPListen:    viper.GetString("http-listen"),
		ClickHouseDSN: viper.GetString("clickhouse-dsn"),
		PostgresDSN:   viper.GetString("postgres-dsn"),
		BatchSize:     viper.GetInt("batch-size"),
		FlushInterval: viper.GetDuration("flush-interval"),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c, err := collector.New(cfg)
	if err != nil {
		return fmt.Errorf("creating collector: %w", err)
	}

	if err := c.Start(ctx); err != nil {
		return fmt.Errorf("starting collector: %w", err)
	}

	// Wait for shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Info().Msg("Shutting down collector...")
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := c.Stop(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("Error during shutdown")
		return err
	}

	log.Info().Msg("Collector stopped")
	return nil
}
