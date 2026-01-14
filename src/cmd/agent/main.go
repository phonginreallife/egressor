// Egressor Agent - Node daemon for traffic collection
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

	"github.com/egressor/egressor/src/internal/agent"
)

var (
	Version   = "dev"
	BuildTime = "unknown"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "egressor-agent",
		Short: "Egressor Agent - Node daemon for traffic collection",
		Long: `Egressor Agent runs as a DaemonSet on each Kubernetes node.
It collects network flow data using eBPF, enriches it with Kubernetes
metadata, and exports it to the Egressor collector.`,
		RunE: run,
	}

	// Flags
	rootCmd.Flags().String("config", "", "Config file path")
	rootCmd.Flags().String("collector-endpoint", "egressor-collector:4317", "Collector gRPC endpoint")
	rootCmd.Flags().String("cgroup-path", "/sys/fs/cgroup", "Cgroup v2 mount path")
	rootCmd.Flags().String("node-name", "", "Kubernetes node name (from downward API)")
	rootCmd.Flags().String("cluster-name", "", "Kubernetes cluster name")
	rootCmd.Flags().StringSlice("cluster-cidrs", []string{"10.0.0.0/8", "172.16.0.0/12"}, "Cluster CIDR ranges")
	rootCmd.Flags().Duration("export-interval", 30*time.Second, "Interval to export flow data")
	rootCmd.Flags().Bool("debug", false, "Enable debug logging")

	// Bind to viper
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
		Msg("Starting Egressor Agent")

	// Load config file if specified
	if configFile := viper.GetString("config"); configFile != "" {
		viper.SetConfigFile(configFile)
		if err := viper.ReadInConfig(); err != nil {
			return fmt.Errorf("reading config file: %w", err)
		}
	}

	// Build agent config
	cfg := agent.Config{
		CollectorEndpoint: viper.GetString("collector-endpoint"),
		CgroupPath:        viper.GetString("cgroup-path"),
		NodeName:          viper.GetString("node-name"),
		ClusterName:       viper.GetString("cluster-name"),
		ClusterCIDRs:      viper.GetStringSlice("cluster-cidrs"),
		ExportInterval:    viper.GetDuration("export-interval"),
	}

	// Get node name from environment if not set
	if cfg.NodeName == "" {
		cfg.NodeName = os.Getenv("NODE_NAME")
	}

	// Create and start agent
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a, err := agent.New(cfg)
	if err != nil {
		return fmt.Errorf("creating agent: %w", err)
	}

	if err := a.Start(ctx); err != nil {
		return fmt.Errorf("starting agent: %w", err)
	}

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Info().Msg("Shutting down agent...")
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := a.Stop(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("Error during shutdown")
		return err
	}

	log.Info().Msg("Agent stopped")
	return nil
}
