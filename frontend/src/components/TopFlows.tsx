'use client'

import { useQuery } from '@tanstack/react-query'
import { ArrowRight, ExternalLink } from 'lucide-react'

interface Flow {
  source: string
  target: string
  transfer_type: string
  total_bytes: number
  cost_usd: number
}

async function fetchTopFlows(): Promise<Flow[]> {
  const res = await fetch('/api/v1/graph/top-edges?n=5')
  if (!res.ok) return getMockFlows()
  return res.json()
}

function getMockFlows(): Flow[] {
  return [
    { source: 'default/api-gateway', target: 'default/user-service', transfer_type: 'pod_to_pod', total_bytes: 45000000000, cost_usd: 0 },
    { source: 'default/user-service', target: 'external:s3.amazonaws.com', transfer_type: 'egress', total_bytes: 32000000000, cost_usd: 2.88 },
    { source: 'prod/order-service', target: 'prod/payment-service', transfer_type: 'cross_az', total_bytes: 28000000000, cost_usd: 0.28 },
    { source: 'default/analytics', target: 'external:bigquery.googleapis.com', transfer_type: 'egress', total_bytes: 21000000000, cost_usd: 1.89 },
    { source: 'infra/prometheus', target: 'infra/grafana', transfer_type: 'pod_to_pod', total_bytes: 15000000000, cost_usd: 0 },
  ]
}

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return `${parseFloat((bytes / Math.pow(k, i)).toFixed(1))} ${sizes[i]}`
}

const typeColors: Record<string, string> = {
  egress: 'text-flow-egress border-flow-egress/30 bg-flow-egress/10',
  cross_az: 'text-flow-warning border-flow-warning/30 bg-flow-warning/10',
  cross_region: 'text-flow-danger border-flow-danger/30 bg-flow-danger/10',
  pod_to_pod: 'text-flow-accent border-flow-accent/30 bg-flow-accent/10',
}

const typeLabels: Record<string, string> = {
  egress: 'EGRESS',
  cross_az: 'CROSS-AZ',
  cross_region: 'CROSS-REGION',
  pod_to_pod: 'INTERNAL',
}

export function TopFlows() {
  const { data: flows = [] } = useQuery({ 
    queryKey: ['topFlows'], 
    queryFn: fetchTopFlows,
    refetchInterval: 2000, // Real-time updates
  })

  return (
    <div className="space-y-3">
      {flows.map((flow, i) => (
        <div 
          key={i} 
          className="flex items-center gap-3 p-3 rounded-lg bg-flow-bg/50 border border-flow-border hover:border-flow-accent/30 transition-colors"
        >
          <div className="flex-1 min-w-0">
            <div className="flex items-center gap-2 text-sm">
              <span className="text-flow-text truncate">{flow.source.split('/')[1]}</span>
              <ArrowRight className="w-4 h-4 text-flow-muted flex-shrink-0" />
              <span className={`truncate ${flow.transfer_type === 'egress' ? 'text-flow-egress' : 'text-flow-text'}`}>
                {flow.target.includes('external:') 
                  ? flow.target.replace('external:', '') 
                  : flow.target.split('/')[1]}
              </span>
              {flow.transfer_type === 'egress' && (
                <ExternalLink className="w-3 h-3 text-flow-egress flex-shrink-0" />
              )}
            </div>
            <div className="flex items-center gap-2 mt-1">
              <span className={`text-xs px-2 py-0.5 rounded border ${typeColors[flow.transfer_type] || typeColors.pod_to_pod}`}>
                {typeLabels[flow.transfer_type] || flow.transfer_type.toUpperCase()}
              </span>
            </div>
          </div>
          
          <div className="text-right flex-shrink-0">
            <p className="text-sm font-medium text-flow-text">{formatBytes(flow.total_bytes)}</p>
            {flow.cost_usd > 0 && (
              <p className="text-xs text-flow-egress">${flow.cost_usd.toFixed(2)}</p>
            )}
          </div>
        </div>
      ))}
      
      {flows.length === 0 && (
        <div className="text-center py-8 text-flow-muted">
          No flow data available
        </div>
      )}
    </div>
  )
}
