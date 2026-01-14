// Package engine implements the FlowScope analytics engine.
package engine

import (
	"context"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/egressor/egressor/src/pkg/types"
)

// BaselineEngine manages behavioral baselines and anomaly detection.
type BaselineEngine struct {
	baselines       map[string]*types.Baseline
	anomalies       []*types.Anomaly
	thresholdStdDev float64
	mu              sync.RWMutex
}

// NewBaselineEngine creates a new baseline engine.
func NewBaselineEngine(thresholdStdDev float64) *BaselineEngine {
	if thresholdStdDev <= 0 {
		thresholdStdDev = 3.0
	}
	return &BaselineEngine{
		baselines:       make(map[string]*types.Baseline),
		thresholdStdDev: thresholdStdDev,
	}
}

// BuildBaseline builds a baseline from historical flow data.
func (e *BaselineEngine) BuildBaseline(
	ctx context.Context,
	flowKey string,
	hourlyValues []float64,
	start, end time.Time,
) *types.Baseline {
	if len(hourlyValues) < 24 { // Need at least 24 hours of data
		return nil
	}

	baseline := &types.Baseline{
		ID:            uuid.New(),
		SourceService: flowKey, // Will be parsed later
		BaselineStart: start,
		BaselineEnd:   end,
		SampleCount:   len(hourlyValues),
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	// Calculate statistics
	baseline.BytesPerHourMean = mean(hourlyValues)
	baseline.BytesPerHourStdDev = stddev(hourlyValues, baseline.BytesPerHourMean)
	baseline.BytesPerHourMedian = median(hourlyValues)
	baseline.BytesPerHourP95 = percentile(hourlyValues, 95)
	baseline.BytesPerHourP99 = percentile(hourlyValues, 99)
	baseline.BytesPerHourMax = max(hourlyValues)

	// Calculate hourly pattern (average by hour of day)
	hourlyPattern := make([]float64, 24)
	hourlyCounts := make([]int, 24)
	for i, v := range hourlyValues {
		hour := i % 24
		hourlyPattern[hour] += v
		hourlyCounts[hour]++
	}
	for i := range hourlyPattern {
		if hourlyCounts[i] > 0 {
			hourlyPattern[i] /= float64(hourlyCounts[i])
		}
	}
	baseline.HourlyPattern = hourlyPattern

	// Calculate daily pattern (average by day of week)
	dailyPattern := make([]float64, 7)
	dailyCounts := make([]int, 7)
	for i, v := range hourlyValues {
		day := (i / 24) % 7
		dailyPattern[day] += v
		dailyCounts[day]++
	}
	for i := range dailyPattern {
		if dailyCounts[i] > 0 {
			dailyPattern[i] /= float64(dailyCounts[i])
		}
	}
	baseline.DailyPattern = dailyPattern

	e.mu.Lock()
	e.baselines[flowKey] = baseline
	e.mu.Unlock()

	log.Info().
		Str("flow", flowKey).
		Float64("mean", baseline.BytesPerHourMean).
		Float64("stddev", baseline.BytesPerHourStdDev).
		Int("samples", baseline.SampleCount).
		Msg("Baseline built")

	return baseline
}

// DetectAnomalies checks current values against baselines.
func (e *BaselineEngine) DetectAnomalies(
	ctx context.Context,
	currentFlows map[string]float64,
) []*types.Anomaly {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var anomalies []*types.Anomaly

	for flowKey, currentValue := range currentFlows {
		baseline, ok := e.baselines[flowKey]
		if !ok {
			// Check if this is a new endpoint
			if currentValue > 0 {
				anomaly := &types.Anomaly{
					ID:             uuid.New(),
					Type:           types.AnomalyTypeNewEndpoint,
					Severity:       types.SeverityInfo,
					SourceService:  flowKey,
					DetectedAt:     time.Now(),
					CurrentValue:   currentValue,
					BaselineValue:  0,
					Deviation:      0,
					AbsoluteDelta:  currentValue,
					CreatedAt:      time.Now(),
					UpdatedAt:      time.Now(),
				}
				anomalies = append(anomalies, anomaly)
			}
			continue
		}

		if baseline.IsAnomalous(currentValue, e.thresholdStdDev) {
			anomaly := e.createAnomaly(flowKey, baseline, currentValue)
			anomalies = append(anomalies, anomaly)
		}
	}

	return anomalies
}

// createAnomaly creates an anomaly from baseline deviation.
func (e *BaselineEngine) createAnomaly(
	flowKey string,
	baseline *types.Baseline,
	currentValue float64,
) *types.Anomaly {
	deviation := 0.0
	if baseline.BytesPerHourStdDev > 0 {
		deviation = (currentValue - baseline.BytesPerHourMean) / baseline.BytesPerHourStdDev
	}

	absoluteDelta := currentValue - baseline.BytesPerHourMean

	// Determine anomaly type
	anomalyType := types.AnomalyTypeSpike
	if deviation > 0 && deviation < 5 {
		// Gradual increase
		anomalyType = types.AnomalyTypeSlowBurn
	}

	// Determine severity
	severity := types.SeverityLow
	absDeviation := math.Abs(deviation)
	if absDeviation > 10 {
		severity = types.SeverityCritical
	} else if absDeviation > 7 {
		severity = types.SeverityHigh
	} else if absDeviation > 5 {
		severity = types.SeverityMedium
	}

	// Estimate cost impact (rough estimate: $0.09/GB egress)
	deltaGB := absoluteDelta / (1024 * 1024 * 1024)
	estimatedCostImpact := deltaGB * 0.09
	estimatedMonthlyImpact := estimatedCostImpact * 24 * 30 // Per hour to monthly

	return &types.Anomaly{
		ID:                        uuid.New(),
		Type:                      anomalyType,
		Severity:                  severity,
		SourceService:             flowKey,
		DetectedAt:                time.Now(),
		CurrentValue:              currentValue,
		BaselineValue:             baseline.BytesPerHourMean,
		Deviation:                 deviation,
		AbsoluteDelta:             absoluteDelta,
		EstimatedCostImpactUSD:    estimatedCostImpact,
		EstimatedMonthlyImpactUSD: estimatedMonthlyImpact,
		CreatedAt:                 time.Now(),
		UpdatedAt:                 time.Now(),
	}
}

// GetBaseline returns baseline for a flow key.
func (e *BaselineEngine) GetBaseline(flowKey string) *types.Baseline {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.baselines[flowKey]
}

// GetAllBaselines returns all baselines.
func (e *BaselineEngine) GetAllBaselines() []*types.Baseline {
	e.mu.RLock()
	defer e.mu.RUnlock()

	baselines := make([]*types.Baseline, 0, len(e.baselines))
	for _, b := range e.baselines {
		baselines = append(baselines, b)
	}
	return baselines
}

// GetActiveAnomalies returns active (unresolved) anomalies.
func (e *BaselineEngine) GetActiveAnomalies() []*types.Anomaly {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var active []*types.Anomaly
	for _, a := range e.anomalies {
		if a.IsActive() {
			active = append(active, a)
		}
	}
	return active
}

// AddAnomaly adds a detected anomaly.
func (e *BaselineEngine) AddAnomaly(anomaly *types.Anomaly) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.anomalies = append(e.anomalies, anomaly)
}

// AcknowledgeAnomaly marks an anomaly as acknowledged.
func (e *BaselineEngine) AcknowledgeAnomaly(anomalyID uuid.UUID, acknowledgedBy string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	for _, a := range e.anomalies {
		if a.ID == anomalyID {
			now := time.Now()
			a.Acknowledged = true
			a.AcknowledgedBy = acknowledgedBy
			a.AcknowledgedAt = &now
			a.UpdatedAt = now
			return nil
		}
	}
	return nil
}

// ResolveAnomaly marks an anomaly as resolved.
func (e *BaselineEngine) ResolveAnomaly(anomalyID uuid.UUID, notes string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	for _, a := range e.anomalies {
		if a.ID == anomalyID {
			now := time.Now()
			a.Resolved = true
			a.ResolvedAt = &now
			a.EndedAt = &now
			a.ResolutionNotes = notes
			a.UpdatedAt = now
			return nil
		}
	}
	return nil
}

// GetAnomalySummary returns summary of anomalies.
func (e *BaselineEngine) GetAnomalySummary() types.AnomalySummary {
	e.mu.RLock()
	defer e.mu.RUnlock()

	summary := types.AnomalySummary{
		BySeverity: make(map[types.Severity]int),
		ByType:     make(map[types.AnomalyType]int),
	}

	for _, a := range e.anomalies {
		if a.IsActive() {
			summary.TotalActive++
			summary.TotalCostImpactUSD += a.EstimatedCostImpactUSD
		} else {
			summary.TotalResolved++
		}

		summary.BySeverity[a.Severity]++
		summary.ByType[a.Type]++
	}

	// Get top anomalies by cost impact
	sorted := make([]*types.Anomaly, len(e.anomalies))
	copy(sorted, e.anomalies)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].EstimatedCostImpactUSD > sorted[j].EstimatedCostImpactUSD
	})

	for i, a := range sorted {
		if i >= 10 {
			break
		}
		summary.TopAnomalies = append(summary.TopAnomalies, *a)
	}

	return summary
}

// Helper functions for statistics

func mean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var sum float64
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

func stddev(values []float64, mean float64) float64 {
	if len(values) < 2 {
		return 0
	}
	var sum float64
	for _, v := range values {
		diff := v - mean
		sum += diff * diff
	}
	return math.Sqrt(sum / float64(len(values)-1))
}

func median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)
	mid := len(sorted) / 2
	if len(sorted)%2 == 0 {
		return (sorted[mid-1] + sorted[mid]) / 2
	}
	return sorted[mid]
}

func percentile(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)
	k := (p / 100) * float64(len(sorted)-1)
	f := int(k)
	c := f + 1
	if c >= len(sorted) {
		return sorted[len(sorted)-1]
	}
	return sorted[f] + (sorted[c]-sorted[f])*(k-float64(f))
}

func max(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	m := values[0]
	for _, v := range values[1:] {
		if v > m {
			m = v
		}
	}
	return m
}
