// Package types defines core data types for FlowScope.
package types

import (
	"time"

	"github.com/google/uuid"
)

// CloudProvider represents supported cloud providers.
type CloudProvider string

const (
	CloudProviderAWS     CloudProvider = "aws"
	CloudProviderGCP     CloudProvider = "gcp"
	CloudProviderAzure   CloudProvider = "azure"
	CloudProviderUnknown CloudProvider = "unknown"
)

// CostCategory categorizes network transfer costs.
type CostCategory string

const (
	CostCategoryEgressInternet CostCategory = "egress_internet"
	CostCategoryEgressRegion   CostCategory = "egress_region"
	CostCategoryCrossAZ        CostCategory = "cross_az"
	CostCategoryCrossRegion    CostCategory = "cross_region"
	CostCategoryVPCPeering     CostCategory = "vpc_peering"
	CostCategoryNATGateway     CostCategory = "nat_gateway"
	CostCategoryLoadBalancer   CostCategory = "load_balancer"
	CostCategoryPrivateLink    CostCategory = "private_link"
)

// PricingTier represents a tiered pricing level.
type PricingTier struct {
	ThresholdGB float64 `json:"threshold_gb"`
	CostPerGB   float64 `json:"cost_per_gb"`
}

// PricingRule defines pricing for a specific transfer type.
type PricingRule struct {
	ID                uuid.UUID     `json:"id"`
	Name              string        `json:"name"`
	Description       string        `json:"description"`
	CloudProvider     CloudProvider `json:"cloud_provider"`
	SourceRegion      string        `json:"source_region,omitempty"`
	DestinationRegion string        `json:"destination_region,omitempty"`
	Category          CostCategory  `json:"category"`
	CostPerGB         float64       `json:"cost_per_gb"`
	FreeTierGB        float64       `json:"free_tier_gb"`
	Tiers             []PricingTier `json:"tiers,omitempty"`
	EffectiveFrom     time.Time     `json:"effective_from"`
	EffectiveUntil    *time.Time    `json:"effective_until,omitempty"`
}

// CalculateCost computes cost for given bytes, accounting for tiers and free tier.
func (p PricingRule) CalculateCost(bytesTransferred uint64, alreadyUsedGB float64) float64 {
	gb := float64(bytesTransferred) / (1024 * 1024 * 1024)
	totalGB := alreadyUsedGB + gb

	// Check free tier
	if totalGB <= p.FreeTierGB {
		return 0.0
	}

	// Calculate billable GB
	billableStart := max(alreadyUsedGB, p.FreeTierGB)
	billableGB := totalGB - billableStart

	if len(p.Tiers) == 0 {
		return billableGB * p.CostPerGB
	}

	// Apply tiered pricing
	var cost float64
	remainingGB := billableGB
	currentPosition := billableStart

	for _, tier := range p.Tiers {
		if currentPosition >= tier.ThresholdGB {
			continue
		}
		tierGB := min(remainingGB, tier.ThresholdGB-currentPosition)
		cost += tierGB * tier.CostPerGB
		remainingGB -= tierGB
		currentPosition += tierGB
		if remainingGB <= 0 {
			break
		}
	}

	// Any remaining at base rate
	if remainingGB > 0 {
		cost += remainingGB * p.CostPerGB
	}

	return cost
}

// CostBreakdown provides detailed cost information for a transfer.
type CostBreakdown struct {
	Category           CostCategory `json:"category"`
	BytesTransferred   uint64       `json:"bytes_transferred"`
	CostUSD            float64      `json:"cost_usd"`
	PricingRuleID      *uuid.UUID   `json:"pricing_rule_id,omitempty"`
	SourceService      string       `json:"source_service,omitempty"`
	DestinationService string       `json:"destination_service,omitempty"`
	SourceRegion       string       `json:"source_region,omitempty"`
	DestinationRegion  string       `json:"destination_region,omitempty"`
}

// CostAttribution attributes costs to specific workloads or dimensions.
type CostAttribution struct {
	ID                uuid.UUID       `json:"id"`
	PeriodStart       time.Time       `json:"period_start"`
	PeriodEnd         time.Time       `json:"period_end"`
	Namespace         string          `json:"namespace,omitempty"`
	ServiceName       string          `json:"service_name,omitempty"`
	DeploymentVersion string          `json:"deployment_version,omitempty"`
	Team              string          `json:"team,omitempty"`
	Environment       string          `json:"environment,omitempty"`
	TotalBytes        uint64          `json:"total_bytes"`
	TotalCostUSD      float64         `json:"total_cost_usd"`
	Breakdown         []CostBreakdown `json:"breakdown"`
	BaselineCostUSD   *float64        `json:"baseline_cost_usd,omitempty"`
	CostDeltaUSD      *float64        `json:"cost_delta_usd,omitempty"`
	CostDeltaPercent  *float64        `json:"cost_delta_percent,omitempty"`
}

// CostPerGB returns average cost per GB for this attribution.
func (c CostAttribution) CostPerGB() float64 {
	if c.TotalBytes == 0 {
		return 0.0
	}
	gb := float64(c.TotalBytes) / (1024 * 1024 * 1024)
	if gb == 0 {
		return 0.0
	}
	return c.TotalCostUSD / gb
}

// IsAnomalous checks if cost is significantly different from baseline.
func (c CostAttribution) IsAnomalous() bool {
	if c.CostDeltaPercent == nil {
		return false
	}
	delta := *c.CostDeltaPercent
	if delta < 0 {
		delta = -delta
	}
	return delta > 50.0 // >50% change
}

// CostSummary provides a high-level cost overview.
type CostSummary struct {
	TotalCostUSD       float64                  `json:"total_cost_usd"`
	TotalBytes         uint64                   `json:"total_bytes"`
	EgressCostUSD      float64                  `json:"egress_cost_usd"`
	CrossRegionCostUSD float64                  `json:"cross_region_cost_usd"`
	CrossAZCostUSD     float64                  `json:"cross_az_cost_usd"`
	ByNamespace        map[string]float64       `json:"by_namespace"`
	ByService          map[string]float64       `json:"by_service"`
	ByCategory         map[CostCategory]float64 `json:"by_category"`
	TopCostDrivers     []CostAttribution        `json:"top_cost_drivers"`
}
