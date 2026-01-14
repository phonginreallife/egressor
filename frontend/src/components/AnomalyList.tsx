'use client'

import { useQuery } from '@tanstack/react-query'
import { AlertTriangle, Zap, TrendingUp, Globe, Clock } from 'lucide-react'
import { formatDistanceToNow } from 'date-fns'

interface Anomaly {
  id: string
  type: string
  severity: string
  source_service: string
  destination_service?: string
  detected_at: string
  current_value: number
  baseline_value: number
  deviation: number
  estimated_cost_impact_usd: number
  ai_summary?: string
}

interface AnomalyListProps {
  limit?: number
}

async function fetchAnomalies(): Promise<Anomaly[]> {
  const res = await fetch('/api/v1/anomalies')
  if (!res.ok) return getMockAnomalies()
  return res.json()
}

function getMockAnomalies(): Anomaly[] {
  return [
    {
      id: '1',
      type: 'spike',
      severity: 'high',
      source_service: 'prod/order-service',
      destination_service: 'external:analytics.google.com',
      detected_at: new Date(Date.now() - 30 * 60 * 1000).toISOString(),
      current_value: 5200000000,
      baseline_value: 1200000000,
      deviation: 4.2,
      estimated_cost_impact_usd: 3.60,
      ai_summary: 'Unusual spike in analytics data export, 4.3x above baseline'
    },
    {
      id: '2',
      type: 'new_endpoint',
      severity: 'medium',
      source_service: 'default/api-gateway',
      destination_service: 'external:unknown-api.com',
      detected_at: new Date(Date.now() - 2 * 60 * 60 * 1000).toISOString(),
      current_value: 850000000,
      baseline_value: 0,
      deviation: 0,
      estimated_cost_impact_usd: 0.08,
      ai_summary: 'New external endpoint detected - requires investigation'
    },
    {
      id: '3',
      type: 'slow_burn',
      severity: 'low',
      source_service: 'infra/prometheus',
      destination_service: 'infra/thanos',
      detected_at: new Date(Date.now() - 6 * 60 * 60 * 1000).toISOString(),
      current_value: 2100000000,
      baseline_value: 1500000000,
      deviation: 1.8,
      estimated_cost_impact_usd: 0.06,
      ai_summary: 'Gradual increase in metrics transfer over past 6 hours'
    },
  ]
}

const severityConfig = {
  critical: { color: 'text-flow-danger bg-flow-danger/10 border-flow-danger/30', icon: Zap },
  high: { color: 'text-flow-egress bg-flow-egress/10 border-flow-egress/30', icon: AlertTriangle },
  medium: { color: 'text-flow-warning bg-flow-warning/10 border-flow-warning/30', icon: TrendingUp },
  low: { color: 'text-flow-info bg-flow-info/10 border-flow-info/30', icon: Globe },
  info: { color: 'text-flow-muted bg-flow-muted/10 border-flow-muted/30', icon: Globe },
}

const typeLabels: Record<string, string> = {
  spike: 'Traffic Spike',
  slow_burn: 'Gradual Increase',
  new_endpoint: 'New Endpoint',
  size_anomaly: 'Size Anomaly',
  cost_anomaly: 'Cost Anomaly',
}

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return `${parseFloat((bytes / Math.pow(k, i)).toFixed(1))} ${sizes[i]}`
}

export function AnomalyList({ limit }: AnomalyListProps) {
  const { data: anomalies = [] } = useQuery({
    queryKey: ['anomalyList'],
    queryFn: fetchAnomalies,
    refetchInterval: 2000, // Real-time updates
  })

  const displayAnomalies = limit ? anomalies.slice(0, limit) : anomalies

  return (
    <div className="space-y-3">
      {displayAnomalies.map((anomaly) => {
        const config = severityConfig[anomaly.severity as keyof typeof severityConfig] || severityConfig.info
        const Icon = config.icon

        return (
          <div
            key={anomaly.id}
            className={`p-4 rounded-lg border ${config.color} transition-all hover:scale-[1.01]`}
          >
            <div className="flex items-start justify-between gap-4">
              <div className="flex items-start gap-3">
                <div className={`p-2 rounded-lg ${config.color}`}>
                  <Icon className="w-4 h-4" />
                </div>
                <div className="space-y-1">
                  <div className="flex items-center gap-2">
                    <span className="text-sm font-medium text-flow-text">
                      {typeLabels[anomaly.type] || anomaly.type}
                    </span>
                    <span className={`text-xs px-2 py-0.5 rounded uppercase ${config.color}`}>
                      {anomaly.severity}
                    </span>
                  </div>
                  <p className="text-sm text-flow-muted">
                    {anomaly.source_service}
                    {anomaly.destination_service && (
                      <span className="text-flow-text"> → {anomaly.destination_service}</span>
                    )}
                  </p>
                  {anomaly.ai_summary && (
                    <p className="text-xs text-flow-muted mt-2 italic">
                      "{anomaly.ai_summary}"
                    </p>
                  )}
                </div>
              </div>

              <div className="text-right flex-shrink-0 space-y-1">
                <p className="text-sm font-medium text-flow-text">
                  {formatBytes(anomaly.current_value)}
                </p>
                {anomaly.baseline_value > 0 && (
                  <p className="text-xs text-flow-muted">
                    baseline: {formatBytes(anomaly.baseline_value)}
                  </p>
                )}
                {anomaly.deviation > 0 && (
                  <p className="text-xs text-flow-egress">
                    +{((anomaly.deviation - 1) * 100).toFixed(0)}% above normal
                  </p>
                )}
                <div className="flex items-center gap-1 text-xs text-flow-muted justify-end mt-2">
                  <Clock className="w-3 h-3" />
                  {formatDistanceToNow(new Date(anomaly.detected_at), { addSuffix: true })}
                </div>
              </div>
            </div>
          </div>
        )
      })}

      {displayAnomalies.length === 0 && (
        <div className="text-center py-8 text-flow-muted">
          <AlertTriangle className="w-8 h-8 mx-auto mb-2 opacity-50" />
          <p>No anomalies detected</p>
        </div>
      )}
    </div>
  )
}
