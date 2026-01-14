// Package storage implements data storage for FlowScope.
package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/rs/zerolog/log"

	"github.com/egressor/egressor/src/pkg/types"
)

// ClickHouseStore implements storage using ClickHouse.
type ClickHouseStore struct {
	conn driver.Conn
}

// NewClickHouseStore creates a new ClickHouse store.
func NewClickHouseStore(dsn string) (*ClickHouseStore, error) {
	opts, err := clickhouse.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("parsing DSN: %w", err)
	}

	conn, err := clickhouse.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("opening connection: %w", err)
	}

	if err := conn.Ping(context.Background()); err != nil {
		return nil, fmt.Errorf("pinging ClickHouse: %w", err)
	}

	store := &ClickHouseStore{conn: conn}

	// Initialize schema
	if err := store.initSchema(context.Background()); err != nil {
		return nil, fmt.Errorf("initializing schema: %w", err)
	}

	log.Info().Msg("Connected to ClickHouse")
	return store, nil
}

// initSchema creates the required tables.
func (s *ClickHouseStore) initSchema(ctx context.Context) error {
	// Transfer events table - main fact table
	eventsTable := `
	CREATE TABLE IF NOT EXISTS transfer_events (
		id UUID,
		timestamp DateTime64(3),
		
		-- Source
		src_ip String,
		src_port UInt16,
		src_type LowCardinality(String),
		src_namespace LowCardinality(String),
		src_service LowCardinality(String),
		src_pod String,
		src_node LowCardinality(String),
		src_cluster LowCardinality(String),
		src_az LowCardinality(String),
		src_region LowCardinality(String),
		
		-- Destination
		dst_ip String,
		dst_port UInt16,
		dst_type LowCardinality(String),
		dst_namespace LowCardinality(String),
		dst_service LowCardinality(String),
		dst_pod String,
		dst_node LowCardinality(String),
		dst_cluster LowCardinality(String),
		dst_az LowCardinality(String),
		dst_region LowCardinality(String),
		dst_hostname String,
		dst_is_internet UInt8,
		dst_cloud_service LowCardinality(String),
		
		-- Transfer metadata
		protocol LowCardinality(String),
		direction LowCardinality(String),
		transfer_type LowCardinality(String),
		
		-- Metrics
		bytes_sent UInt64,
		bytes_received UInt64,
		packets_sent UInt64,
		packets_received UInt64,
		duration_ns UInt64,
		
		-- Request context
		http_method LowCardinality(String),
		http_path String,
		http_status_code UInt16,
		grpc_method String,
		
		-- Tracing
		trace_id String,
		span_id String,
		
		-- Labels (stored as JSON)
		labels String
	) ENGINE = MergeTree()
	PARTITION BY toYYYYMM(timestamp)
	ORDER BY (timestamp, src_namespace, src_service, dst_namespace, dst_service)
	TTL timestamp + INTERVAL 30 DAY
	`

	if err := s.conn.Exec(ctx, eventsTable); err != nil {
		return fmt.Errorf("creating events table: %w", err)
	}

	// Aggregated flows table - hourly aggregates
	flowsTable := `
	CREATE TABLE IF NOT EXISTS transfer_flows_hourly (
		hour DateTime,
		src_namespace LowCardinality(String),
		src_service LowCardinality(String),
		dst_namespace LowCardinality(String),
		dst_service LowCardinality(String),
		dst_external String,
		transfer_type LowCardinality(String),
		
		total_bytes AggregateFunction(sum, UInt64),
		total_packets AggregateFunction(sum, UInt64),
		event_count AggregateFunction(count, UInt64),
		bytes_avg AggregateFunction(avg, UInt64),
		bytes_max AggregateFunction(max, UInt64)
	) ENGINE = AggregatingMergeTree()
	PARTITION BY toYYYYMM(hour)
	ORDER BY (hour, src_namespace, src_service, dst_namespace, dst_service)
	TTL hour + INTERVAL 90 DAY
	`

	if err := s.conn.Exec(ctx, flowsTable); err != nil {
		return fmt.Errorf("creating flows table: %w", err)
	}

	// Materialized view for automatic aggregation
	flowsMV := `
	CREATE MATERIALIZED VIEW IF NOT EXISTS transfer_flows_hourly_mv
	TO transfer_flows_hourly AS
	SELECT
		toStartOfHour(timestamp) AS hour,
		src_namespace,
		src_service,
		dst_namespace,
		dst_service,
		if(dst_is_internet = 1, dst_ip, '') AS dst_external,
		transfer_type,
		sumState(bytes_sent + bytes_received) AS total_bytes,
		sumState(packets_sent + packets_received) AS total_packets,
		countState() AS event_count,
		avgState(bytes_sent + bytes_received) AS bytes_avg,
		maxState(bytes_sent + bytes_received) AS bytes_max
	FROM transfer_events
	GROUP BY hour, src_namespace, src_service, dst_namespace, dst_service, dst_external, transfer_type
	`

	if err := s.conn.Exec(ctx, flowsMV); err != nil {
		log.Warn().Err(err).Msg("Flows MV may already exist")
	}

	// Cost tracking table
	costTable := `
	CREATE TABLE IF NOT EXISTS cost_attributions (
		id UUID,
		period_start DateTime,
		period_end DateTime,
		namespace LowCardinality(String),
		service_name LowCardinality(String),
		deployment_version String,
		team LowCardinality(String),
		environment LowCardinality(String),
		
		total_bytes UInt64,
		total_cost_usd Float64,
		
		egress_bytes UInt64,
		egress_cost_usd Float64,
		cross_region_bytes UInt64,
		cross_region_cost_usd Float64,
		cross_az_bytes UInt64,
		cross_az_cost_usd Float64,
		
		baseline_cost_usd Nullable(Float64),
		cost_delta_usd Nullable(Float64),
		cost_delta_percent Nullable(Float64),
		
		created_at DateTime DEFAULT now()
	) ENGINE = MergeTree()
	PARTITION BY toYYYYMM(period_start)
	ORDER BY (period_start, namespace, service_name)
	TTL period_start + INTERVAL 365 DAY
	`

	if err := s.conn.Exec(ctx, costTable); err != nil {
		return fmt.Errorf("creating cost table: %w", err)
	}

	// Anomalies table
	anomaliesTable := `
	CREATE TABLE IF NOT EXISTS anomalies (
		id UUID,
		type LowCardinality(String),
		severity LowCardinality(String),
		src_service LowCardinality(String),
		dst_service LowCardinality(String),
		dst_endpoint String,
		
		detected_at DateTime64(3),
		started_at Nullable(DateTime64(3)),
		ended_at Nullable(DateTime64(3)),
		
		current_value Float64,
		baseline_value Float64,
		deviation Float64,
		absolute_delta Float64,
		
		estimated_cost_impact_usd Float64,
		estimated_monthly_impact_usd Float64,
		
		acknowledged UInt8 DEFAULT 0,
		resolved UInt8 DEFAULT 0,
		ai_summary String,
		
		created_at DateTime DEFAULT now()
	) ENGINE = MergeTree()
	PARTITION BY toYYYYMM(detected_at)
	ORDER BY (detected_at, severity, type)
	TTL detected_at + INTERVAL 180 DAY
	`

	if err := s.conn.Exec(ctx, anomaliesTable); err != nil {
		return fmt.Errorf("creating anomalies table: %w", err)
	}

	// Baselines table
	baselinesTable := `
	CREATE TABLE IF NOT EXISTS baselines (
		id UUID,
		src_service LowCardinality(String),
		dst_service LowCardinality(String),
		dst_endpoint String,
		transfer_type LowCardinality(String),
		
		baseline_start DateTime,
		baseline_end DateTime,
		sample_count UInt32,
		
		bytes_per_hour_mean Float64,
		bytes_per_hour_stddev Float64,
		bytes_per_hour_median Float64,
		bytes_per_hour_p95 Float64,
		bytes_per_hour_p99 Float64,
		bytes_per_hour_max Float64,
		
		hourly_pattern Array(Float64),
		daily_pattern Array(Float64),
		
		created_at DateTime DEFAULT now(),
		updated_at DateTime DEFAULT now()
	) ENGINE = ReplacingMergeTree(updated_at)
	ORDER BY (src_service, dst_service, dst_endpoint, transfer_type)
	`

	if err := s.conn.Exec(ctx, baselinesTable); err != nil {
		return fmt.Errorf("creating baselines table: %w", err)
	}

	log.Info().Msg("ClickHouse schema initialized")
	return nil
}

// InsertEvents inserts a batch of transfer events.
func (s *ClickHouseStore) InsertEvents(ctx context.Context, events []types.TransferEvent) error {
	batch, err := s.conn.PrepareBatch(ctx, `
		INSERT INTO transfer_events (
			id, timestamp,
			src_ip, src_port, src_type, src_namespace, src_service, src_pod, src_node, src_cluster, src_az, src_region,
			dst_ip, dst_port, dst_type, dst_namespace, dst_service, dst_pod, dst_node, dst_cluster, dst_az, dst_region,
			dst_hostname, dst_is_internet, dst_cloud_service,
			protocol, direction, transfer_type,
			bytes_sent, bytes_received, packets_sent, packets_received, duration_ns,
			http_method, http_path, http_status_code, grpc_method,
			trace_id, span_id, labels
		)
	`)
	if err != nil {
		return fmt.Errorf("preparing batch: %w", err)
	}

	for _, e := range events {
		srcIdentity := e.Source.Identity
		dstIdentity := e.Destination.Identity

		isInternet := uint8(0)
		if e.Destination.IsInternet {
			isInternet = 1
		}

		err := batch.Append(
			e.ID, e.Timestamp,
			e.Source.IP, e.Source.Port, string(e.Source.Type),
			getOrEmpty(srcIdentity, func(i *types.ServiceIdentity) string { return i.Namespace }),
			getOrEmpty(srcIdentity, func(i *types.ServiceIdentity) string { return i.Name }),
			getOrEmpty(srcIdentity, func(i *types.ServiceIdentity) string { return i.PodName }),
			getOrEmpty(srcIdentity, func(i *types.ServiceIdentity) string { return i.NodeName }),
			getOrEmpty(srcIdentity, func(i *types.ServiceIdentity) string { return i.Cluster }),
			getOrEmpty(srcIdentity, func(i *types.ServiceIdentity) string { return i.AvailabilityZone }),
			getOrEmpty(srcIdentity, func(i *types.ServiceIdentity) string { return i.Region }),
			e.Destination.IP, e.Destination.Port, string(e.Destination.Type),
			getOrEmpty(dstIdentity, func(i *types.ServiceIdentity) string { return i.Namespace }),
			getOrEmpty(dstIdentity, func(i *types.ServiceIdentity) string { return i.Name }),
			getOrEmpty(dstIdentity, func(i *types.ServiceIdentity) string { return i.PodName }),
			getOrEmpty(dstIdentity, func(i *types.ServiceIdentity) string { return i.NodeName }),
			getOrEmpty(dstIdentity, func(i *types.ServiceIdentity) string { return i.Cluster }),
			getOrEmpty(dstIdentity, func(i *types.ServiceIdentity) string { return i.AvailabilityZone }),
			getOrEmpty(dstIdentity, func(i *types.ServiceIdentity) string { return i.Region }),
			e.Destination.Hostname, isInternet, e.Destination.CloudServiceName,
			e.Protocol, string(e.Direction), string(e.Type),
			e.BytesSent, e.BytesReceived, e.PacketsSent, e.PacketsReceived, e.DurationNs,
			e.HTTPMethod, e.HTTPPath, e.HTTPStatusCode, e.GRPCMethod,
			e.TraceID, e.SpanID, "{}",
		)
		if err != nil {
			return fmt.Errorf("appending to batch: %w", err)
		}
	}

	return batch.Send()
}

// getOrEmpty returns field value or empty string.
func getOrEmpty(identity *types.ServiceIdentity, getter func(*types.ServiceIdentity) string) string {
	if identity == nil {
		return ""
	}
	return getter(identity)
}

// QueryFlows queries aggregated flows.
func (s *ClickHouseStore) QueryFlows(ctx context.Context, query FlowQuery) ([]FlowResult, error) {
	sql := `
		SELECT
			src_namespace,
			src_service,
			dst_namespace,
			dst_service,
			dst_external,
			transfer_type,
			sumMerge(total_bytes) AS total_bytes,
			sumMerge(total_packets) AS total_packets,
			countMerge(event_count) AS event_count
		FROM transfer_flows_hourly
		WHERE hour >= ? AND hour < ?
	`

	args := []interface{}{query.Start, query.End}

	if query.SrcNamespace != "" {
		sql += " AND src_namespace = ?"
		args = append(args, query.SrcNamespace)
	}
	if query.SrcService != "" {
		sql += " AND src_service = ?"
		args = append(args, query.SrcService)
	}
	if query.DstNamespace != "" {
		sql += " AND dst_namespace = ?"
		args = append(args, query.DstNamespace)
	}
	if query.DstService != "" {
		sql += " AND dst_service = ?"
		args = append(args, query.DstService)
	}
	if query.TransferType != "" {
		sql += " AND transfer_type = ?"
		args = append(args, query.TransferType)
	}

	sql += ` GROUP BY src_namespace, src_service, dst_namespace, dst_service, dst_external, transfer_type
	         ORDER BY total_bytes DESC
	         LIMIT ?`
	args = append(args, query.Limit)

	rows, err := s.conn.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("querying flows: %w", err)
	}
	defer rows.Close()

	var results []FlowResult
	for rows.Next() {
		var r FlowResult
		if err := rows.Scan(
			&r.SrcNamespace, &r.SrcService,
			&r.DstNamespace, &r.DstService, &r.DstExternal,
			&r.TransferType,
			&r.TotalBytes, &r.TotalPackets, &r.EventCount,
		); err != nil {
			return nil, fmt.Errorf("scanning row: %w", err)
		}
		results = append(results, r)
	}

	return results, nil
}

// Close closes the connection.
func (s *ClickHouseStore) Close() error {
	return s.conn.Close()
}

// FlowQuery represents a flow query.
type FlowQuery struct {
	Start        time.Time
	End          time.Time
	SrcNamespace string
	SrcService   string
	DstNamespace string
	DstService   string
	TransferType string
	Limit        int
}

// FlowResult represents a flow query result.
type FlowResult struct {
	SrcNamespace string
	SrcService   string
	DstNamespace string
	DstService   string
	DstExternal  string
	TransferType string
	TotalBytes   uint64
	TotalPackets uint64
	EventCount   uint64
}
