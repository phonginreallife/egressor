// Package engine implements the FlowScope analytics engine.
package engine

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/egressor/egressor/src/pkg/types"
)

// CostEngine calculates and attributes data transfer costs.
type CostEngine struct {
	rules      []types.PricingRule
	monthly    map[string]float64 // Track monthly usage per destination region
	mu         sync.RWMutex
}

// NewCostEngine creates a new cost engine with default pricing rules.
func NewCostEngine() *CostEngine {
	engine := &CostEngine{
		monthly: make(map[string]float64),
	}

	// Load default AWS pricing rules
	engine.LoadDefaultAWSPricing()

	return engine
}

// LoadDefaultAWSPricing loads default AWS data transfer pricing.
func (e *CostEngine) LoadDefaultAWSPricing() {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.rules = []types.PricingRule{
		// Internet egress - tiered pricing
		{
			ID:            uuid.New(),
			Name:          "AWS Internet Egress",
			Description:   "Data transfer out to the Internet",
			CloudProvider: types.CloudProviderAWS,
			Category:      types.CostCategoryEgressInternet,
			CostPerGB:     0.09, // Base rate
			FreeTierGB:    1.0,  // First 1GB/month free
			Tiers: []types.PricingTier{
				{ThresholdGB: 10 * 1024, CostPerGB: 0.09},   // First 10TB
				{ThresholdGB: 50 * 1024, CostPerGB: 0.085},  // Next 40TB
				{ThresholdGB: 150 * 1024, CostPerGB: 0.07},  // Next 100TB
			},
			EffectiveFrom: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		// Cross-AZ traffic
		{
			ID:            uuid.New(),
			Name:          "AWS Cross-AZ Transfer",
			Description:   "Data transfer between availability zones",
			CloudProvider: types.CloudProviderAWS,
			Category:      types.CostCategoryCrossAZ,
			CostPerGB:     0.01, // $0.01/GB each direction
			EffectiveFrom: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		// Cross-region transfer (example: us-east-1 to us-west-2)
		{
			ID:              uuid.New(),
			Name:            "AWS Cross-Region US East to West",
			Description:     "Data transfer between US regions",
			CloudProvider:   types.CloudProviderAWS,
			SourceRegion:    "us-east-1",
			DestinationRegion: "us-west-2",
			Category:        types.CostCategoryCrossRegion,
			CostPerGB:       0.02,
			EffectiveFrom:   time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		// NAT Gateway
		{
			ID:            uuid.New(),
			Name:          "AWS NAT Gateway Processing",
			Description:   "NAT Gateway data processing charges",
			CloudProvider: types.CloudProviderAWS,
			Category:      types.CostCategoryNATGateway,
			CostPerGB:     0.045,
			EffectiveFrom: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		// VPC Peering cross-region
		{
			ID:            uuid.New(),
			Name:          "AWS VPC Peering Cross-Region",
			Description:   "VPC peering data transfer cross-region",
			CloudProvider: types.CloudProviderAWS,
			Category:      types.CostCategoryVPCPeering,
			CostPerGB:     0.01,
			EffectiveFrom: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		},
	}
}

// AddPricingRule adds a custom pricing rule.
func (e *CostEngine) AddPricingRule(rule types.PricingRule) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.rules = append(e.rules, rule)
}

// CalculateCost calculates cost for a transfer flow.
func (e *CostEngine) CalculateCost(flow types.TransferFlow) types.CostBreakdown {
	e.mu.RLock()
	defer e.mu.RUnlock()

	category := e.classifyCategory(flow)
	rule := e.findMatchingRule(flow, category)

	var cost float64
	if rule != nil {
		// Track monthly usage for tiered pricing
		monthlyKey := fmt.Sprintf("%s-%s", time.Now().Format("2006-01"), category)
		alreadyUsed := e.monthly[monthlyKey]
		cost = rule.CalculateCost(flow.TotalBytes, alreadyUsed)
	} else {
		// Default pricing
		gb := float64(flow.TotalBytes) / (1024 * 1024 * 1024)
		switch category {
		case types.CostCategoryEgressInternet:
			cost = gb * 0.09
		case types.CostCategoryCrossAZ:
			cost = gb * 0.01
		case types.CostCategoryCrossRegion:
			cost = gb * 0.02
		default:
			cost = 0
		}
	}

	var srcService, dstService string
	srcService = flow.SourceIdentity.FullName()
	if flow.DestinationIdentity != nil {
		dstService = flow.DestinationIdentity.FullName()
	} else if flow.DestinationEndpoint != nil {
		dstService = flow.DestinationEndpoint.IP
	}

	return types.CostBreakdown{
		Category:           category,
		BytesTransferred:   flow.TotalBytes,
		CostUSD:            cost,
		SourceService:      srcService,
		DestinationService: dstService,
	}
}

// classifyCategory determines the cost category for a flow.
func (e *CostEngine) classifyCategory(flow types.TransferFlow) types.CostCategory {
	switch flow.Type {
	case types.TransferTypeEgress:
		return types.CostCategoryEgressInternet
	case types.TransferTypeCrossAZ:
		return types.CostCategoryCrossAZ
	case types.TransferTypeCrossRegion:
		return types.CostCategoryCrossRegion
	default:
		return types.CostCategoryCrossAZ // Internal traffic often crosses AZs
	}
}

// findMatchingRule finds the best matching pricing rule.
func (e *CostEngine) findMatchingRule(flow types.TransferFlow, category types.CostCategory) *types.PricingRule {
	now := time.Now()

	for i := range e.rules {
		rule := &e.rules[i]

		// Check category
		if rule.Category != category {
			continue
		}

		// Check effective dates
		if rule.EffectiveFrom.After(now) {
			continue
		}
		if rule.EffectiveUntil != nil && rule.EffectiveUntil.Before(now) {
			continue
		}

		// Check region matching for cross-region rules
		if rule.SourceRegion != "" || rule.DestinationRegion != "" {
			srcRegion := ""
			dstRegion := ""
			if flow.SourceIdentity.Region != "" {
				srcRegion = flow.SourceIdentity.Region
			}
			if flow.DestinationIdentity != nil && flow.DestinationIdentity.Region != "" {
				dstRegion = flow.DestinationIdentity.Region
			}

			if rule.SourceRegion != "" && rule.SourceRegion != srcRegion {
				continue
			}
			if rule.DestinationRegion != "" && rule.DestinationRegion != dstRegion {
				continue
			}
		}

		return rule
	}

	return nil
}

// CalculateAttribution calculates cost attribution for a time period.
func (e *CostEngine) CalculateAttribution(
	ctx context.Context,
	flows []types.TransferFlow,
	periodStart, periodEnd time.Time,
) []types.CostAttribution {
	// Group flows by service
	byService := make(map[string][]types.TransferFlow)
	for _, flow := range flows {
		key := flow.SourceIdentity.FullName()
		byService[key] = append(byService[key], flow)
	}

	var attributions []types.CostAttribution

	for serviceKey, serviceFlows := range byService {
		attr := types.CostAttribution{
			ID:          uuid.New(),
			PeriodStart: periodStart,
			PeriodEnd:   periodEnd,
			Namespace:   serviceFlows[0].SourceIdentity.Namespace,
			ServiceName: serviceFlows[0].SourceIdentity.Name,
			Team:        serviceFlows[0].SourceIdentity.Team,
			Environment: serviceFlows[0].SourceIdentity.Environment,
		}

		var breakdowns []types.CostBreakdown
		for _, flow := range serviceFlows {
			attr.TotalBytes += flow.TotalBytes

			breakdown := e.CalculateCost(flow)
			attr.TotalCostUSD += breakdown.CostUSD
			breakdowns = append(breakdowns, breakdown)
		}

		attr.Breakdown = breakdowns
		attributions = append(attributions, attr)

		log.Debug().
			Str("service", serviceKey).
			Uint64("bytes", attr.TotalBytes).
			Float64("cost_usd", attr.TotalCostUSD).
			Msg("Cost attribution calculated")
	}

	return attributions
}

// GetCostSummary calculates a cost summary for a time period.
func (e *CostEngine) GetCostSummary(
	attributions []types.CostAttribution,
) types.CostSummary {
	summary := types.CostSummary{
		ByNamespace: make(map[string]float64),
		ByService:   make(map[string]float64),
		ByCategory:  make(map[types.CostCategory]float64),
	}

	for _, attr := range attributions {
		summary.TotalCostUSD += attr.TotalCostUSD
		summary.TotalBytes += attr.TotalBytes

		if attr.Namespace != "" {
			summary.ByNamespace[attr.Namespace] += attr.TotalCostUSD
		}
		if attr.ServiceName != "" {
			key := attr.Namespace + "/" + attr.ServiceName
			summary.ByService[key] += attr.TotalCostUSD
		}

		for _, b := range attr.Breakdown {
			summary.ByCategory[b.Category] += b.CostUSD

			switch b.Category {
			case types.CostCategoryEgressInternet:
				summary.EgressCostUSD += b.CostUSD
			case types.CostCategoryCrossRegion:
				summary.CrossRegionCostUSD += b.CostUSD
			case types.CostCategoryCrossAZ:
				summary.CrossAZCostUSD += b.CostUSD
			}
		}
	}

	// Get top cost drivers
	for i, attr := range attributions {
		if i >= 10 {
			break
		}
		summary.TopCostDrivers = append(summary.TopCostDrivers, attr)
	}

	return summary
}

// EstimateMonthlyProjection estimates monthly cost based on current data.
func (e *CostEngine) EstimateMonthlyProjection(
	currentCost float64,
	periodDays float64,
) float64 {
	if periodDays <= 0 {
		return 0
	}
	dailyRate := currentCost / periodDays
	return dailyRate * 30
}

// GetPricingRules returns all pricing rules.
func (e *CostEngine) GetPricingRules() []types.PricingRule {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return append([]types.PricingRule{}, e.rules...)
}
