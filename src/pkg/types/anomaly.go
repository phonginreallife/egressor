// Package types defines core data types for FlowScope.
package types

import (
	"time"

	"github.com/google/uuid"
)

// AnomalyType classifies detected anomalies.
type AnomalyType string

const (
	AnomalyTypeSpike             AnomalyType = "spike"
	AnomalyTypeSlowBurn          AnomalyType = "slow_burn"
	AnomalyTypeNewEndpoint       AnomalyType = "new_endpoint"
	AnomalyTypeNewPattern        AnomalyType = "new_pattern"
	AnomalyTypeSizeAnomaly       AnomalyType = "size_anomaly"
	AnomalyTypeFrequencyAnomaly  AnomalyType = "frequency_anomaly"
	AnomalyTypeCostAnomaly       AnomalyType = "cost_anomaly"
	AnomalyTypeLeak              AnomalyType = "leak"
)

// Severity represents anomaly severity levels.
type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityLow      Severity = "low"
	SeverityMedium   Severity = "medium"
	SeverityHigh     Severity = "high"
	SeverityCritical Severity = "critical"
)

// Baseline represents statistical baseline for a transfer pattern.
type Baseline struct {
	ID                   uuid.UUID `json:"id"`
	SourceService        string    `json:"source_service"`
	DestinationService   string    `json:"destination_service,omitempty"`
	DestinationEndpoint  string    `json:"destination_endpoint,omitempty"`
	TransferType         string    `json:"transfer_type"`
	BaselineStart        time.Time `json:"baseline_start"`
	BaselineEnd          time.Time `json:"baseline_end"`
	SampleCount          int       `json:"sample_count"`
	BytesPerHourMean     float64   `json:"bytes_per_hour_mean"`
	BytesPerHourStdDev   float64   `json:"bytes_per_hour_stddev"`
	BytesPerHourMedian   float64   `json:"bytes_per_hour_median"`
	BytesPerHourP95      float64   `json:"bytes_per_hour_p95"`
	BytesPerHourP99      float64   `json:"bytes_per_hour_p99"`
	BytesPerHourMax      float64   `json:"bytes_per_hour_max"`
	RequestsPerHourMean  float64   `json:"requests_per_hour_mean"`
	RequestsPerHourStdDev float64  `json:"requests_per_hour_stddev"`
	RequestSizeMean      float64   `json:"request_size_mean"`
	RequestSizeStdDev    float64   `json:"request_size_stddev"`
	ResponseSizeMean     float64   `json:"response_size_mean"`
	ResponseSizeStdDev   float64   `json:"response_size_stddev"`
	HourlyPattern        []float64 `json:"hourly_pattern"` // 24 values
	DailyPattern         []float64 `json:"daily_pattern"`  // 7 values
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

// IsAnomalous checks if a value is anomalous compared to baseline.
func (b Baseline) IsAnomalous(currentValue float64, thresholdStdDev float64) bool {
	if b.BytesPerHourStdDev == 0 {
		return currentValue > b.BytesPerHourMean*2
	}
	zScore := (currentValue - b.BytesPerHourMean) / b.BytesPerHourStdDev
	if zScore < 0 {
		zScore = -zScore
	}
	return zScore > thresholdStdDev
}

// Anomaly represents a detected anomaly in transfer behavior.
type Anomaly struct {
	ID                       uuid.UUID         `json:"id"`
	Type                     AnomalyType       `json:"type"`
	Severity                 Severity          `json:"severity"`
	SourceService            string            `json:"source_service"`
	DestinationService       string            `json:"destination_service,omitempty"`
	DestinationEndpoint      string            `json:"destination_endpoint,omitempty"`
	DetectedAt               time.Time         `json:"detected_at"`
	StartedAt                *time.Time        `json:"started_at,omitempty"`
	EndedAt                  *time.Time        `json:"ended_at,omitempty"`
	CurrentValue             float64           `json:"current_value"`
	BaselineValue            float64           `json:"baseline_value"`
	Deviation                float64           `json:"deviation"` // Stddevs from baseline
	AbsoluteDelta            float64           `json:"absolute_delta"`
	EstimatedCostImpactUSD   float64           `json:"estimated_cost_impact_usd"`
	EstimatedMonthlyImpactUSD float64          `json:"estimated_monthly_impact_usd"`
	RelatedEventIDs          []string          `json:"related_event_ids,omitempty"`
	PotentialCauses          []string          `json:"potential_causes,omitempty"`
	SuggestedActions         []string          `json:"suggested_actions,omitempty"`
	Acknowledged             bool              `json:"acknowledged"`
	AcknowledgedBy           string            `json:"acknowledged_by,omitempty"`
	AcknowledgedAt           *time.Time        `json:"acknowledged_at,omitempty"`
	Resolved                 bool              `json:"resolved"`
	ResolvedAt               *time.Time        `json:"resolved_at,omitempty"`
	ResolutionNotes          string            `json:"resolution_notes,omitempty"`
	AISummary                string            `json:"ai_summary,omitempty"`
	AIAnalysis               map[string]any    `json:"ai_analysis,omitempty"`
	Labels                   map[string]string `json:"labels,omitempty"`
	CreatedAt                time.Time         `json:"created_at"`
	UpdatedAt                time.Time         `json:"updated_at"`
}

// IsActive checks if anomaly is still active.
func (a Anomaly) IsActive() bool {
	return !a.Resolved && a.EndedAt == nil
}

// DurationHours returns duration of anomaly in hours.
func (a Anomaly) DurationHours() *float64 {
	if a.StartedAt == nil {
		return nil
	}
	var end time.Time
	if a.EndedAt != nil {
		end = *a.EndedAt
	} else {
		end = time.Now()
	}
	hours := end.Sub(*a.StartedAt).Hours()
	return &hours
}

// PercentIncrease returns percentage increase from baseline.
func (a Anomaly) PercentIncrease() float64 {
	if a.BaselineValue == 0 {
		if a.CurrentValue > 0 {
			return 100.0 // Treat as 100% if baseline is 0
		}
		return 0.0
	}
	return ((a.CurrentValue - a.BaselineValue) / a.BaselineValue) * 100
}

// AnomalySummary provides overview of anomaly state.
type AnomalySummary struct {
	TotalActive         int                     `json:"total_active"`
	TotalResolved       int                     `json:"total_resolved"`
	BySeverity          map[Severity]int        `json:"by_severity"`
	ByType              map[AnomalyType]int     `json:"by_type"`
	TotalCostImpactUSD  float64                 `json:"total_cost_impact_usd"`
	TopAnomalies        []Anomaly               `json:"top_anomalies"`
}
