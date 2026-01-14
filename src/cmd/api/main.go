// Egressor API Server
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

	"github.com/egressor/egressor/src/internal/api"
)

var (
	Version   = "dev"
	BuildTime = "unknown"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "egressor-api",
		Short: "Egressor API Server",
		Long: `Egressor API Server provides REST and gRPC APIs for
querying transfer data, costs, anomalies, and AI-powered analysis.`,
		RunE: run,
	}

	// Flags
	rootCmd.Flags().String("config", "", "Config file path")
	rootCmd.Flags().String("http-listen", ":8080", "HTTP listen address")
	rootCmd.Flags().String("grpc-listen", ":9090", "gRPC listen address")
	rootCmd.Flags().String("clickhouse-dsn", "clickhouse://localhost:9000/egressor", "ClickHouse DSN")
	rootCmd.Flags().String("postgres-dsn", "postgres://localhost:5432/egressor", "PostgreSQL DSN")
	rootCmd.Flags().String("intelligence-url", "http://localhost:8090", "Intelligence service URL")
	rootCmd.Flags().StringSlice("cors-origins", []string{"http://localhost:3000"}, "CORS allowed origins")
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
		Msg("Starting Egressor API Server")

	// Load config
	if configFile := viper.GetString("config"); configFile != "" {
		viper.SetConfigFile(configFile)
		if err := viper.ReadInConfig(); err != nil {
			return fmt.Errorf("reading config: %w", err)
		}
	}

	cfg := api.Config{
		HTTPListen:      viper.GetString("http-listen"),
		GRPCListen:      viper.GetString("grpc-listen"),
		ClickHouseDSN:   viper.GetString("clickhouse-dsn"),
		PostgresDSN:     viper.GetString("postgres-dsn"),
		IntelligenceURL: viper.GetString("intelligence-url"),
		CORSOrigins:     viper.GetStringSlice("cors-origins"),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	server, err := api.NewServer(cfg)
	if err != nil {
		return fmt.Errorf("creating server: %w", err)
	}

	if err := server.Start(ctx); err != nil {
		return fmt.Errorf("starting server: %w", err)
	}

	// Wait for shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Info().Msg("Shutting down API server...")
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := server.Stop(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("Error during shutdown")
		return err
	}

	log.Info().Msg("API server stopped")
	return nil
}
