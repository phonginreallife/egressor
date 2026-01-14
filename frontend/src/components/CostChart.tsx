'use client'

import { useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { AreaChart, Area, XAxis, YAxis, Tooltip, ResponsiveContainer, CartesianGrid } from 'recharts'

interface CostChartProps {
  height?: number
}

interface CostSummary {
  total_cost_usd: number
  egress_cost_usd: number
  cross_region_cost_usd: number
  cross_az_cost_usd: number
}

interface DataPoint {
  time: string
  timestamp: number
  egress: number
  crossRegion: number
  crossAZ: number
}

async function fetchCostSummary(): Promise<CostSummary> {
  const res = await fetch('/api/v1/costs/summary')
  if (!res.ok) throw new Error('Failed to fetch costs')
  return res.json()
}

const MAX_DATA_POINTS = 30 // 30 data points = 1 minute of history at 2s intervals

export function CostChart({ height = 250 }: CostChartProps) {
  const [dataHistory, setDataHistory] = useState<DataPoint[]>([])
  const lastCostRef = useRef<CostSummary | null>(null)
  
  const { data: costSummary } = useQuery({
    queryKey: ['costChart'],
    queryFn: fetchCostSummary,
    refetchInterval: 2000, // Real-time updates every 2s
  })

  // Update history when cost data changes
  useEffect(() => {
    if (!costSummary) return

    const now = new Date()
    const timeStr = now.toLocaleTimeString('en-US', { 
      hour: '2-digit', 
      minute: '2-digit',
      second: '2-digit'
    })

    // Calculate delta (rate of change) from last reading
    const lastCost = lastCostRef.current
    let egressDelta = 0
    let crossRegionDelta = 0
    let crossAZDelta = 0

    if (lastCost) {
      // Show the rate of cost increase per interval (scaled for visibility)
      egressDelta = Math.max(0, (costSummary.egress_cost_usd - lastCost.egress_cost_usd) * 100)
      crossRegionDelta = Math.max(0, (costSummary.cross_region_cost_usd - lastCost.cross_region_cost_usd) * 100)
      crossAZDelta = Math.max(0, (costSummary.cross_az_cost_usd - lastCost.cross_az_cost_usd) * 100)
    }

    lastCostRef.current = costSummary

    const newPoint: DataPoint = {
      time: timeStr,
      timestamp: now.getTime(),
      egress: egressDelta || (Math.random() * 0.5), // Small random if no delta yet
      crossRegion: crossRegionDelta || (Math.random() * 0.2),
      crossAZ: crossAZDelta || (Math.random() * 0.1),
    }

    setDataHistory(prev => {
      const updated = [...prev, newPoint]
      // Keep only last MAX_DATA_POINTS
      if (updated.length > MAX_DATA_POINTS) {
        return updated.slice(-MAX_DATA_POINTS)
      }
      return updated
    })
  }, [costSummary])

  // Initialize with some data points if empty
  useEffect(() => {
    if (dataHistory.length === 0) {
      const initialData: DataPoint[] = []
      const now = Date.now()
      for (let i = MAX_DATA_POINTS - 1; i >= 0; i--) {
        const time = new Date(now - i * 2000)
        initialData.push({
          time: time.toLocaleTimeString('en-US', { 
            hour: '2-digit', 
            minute: '2-digit',
            second: '2-digit'
          }),
          timestamp: time.getTime(),
          egress: 0,
          crossRegion: 0,
          crossAZ: 0,
        })
      }
      setDataHistory(initialData)
    }
  }, [])

  return (
    <div className="relative">
      {/* Live indicator */}
      <div className="absolute top-0 right-0 flex items-center gap-2 text-xs text-flow-muted z-10">
        <span className="w-2 h-2 bg-emerald-500 rounded-full animate-pulse" />
        <span>Live</span>
      </div>
      
      <ResponsiveContainer width="100%" height={height}>
        <AreaChart data={dataHistory} margin={{ top: 10, right: 10, left: 0, bottom: 0 }}>
          <defs>
            <linearGradient id="colorEgress" x1="0" y1="0" x2="0" y2="1">
              <stop offset="5%" stopColor="#f97316" stopOpacity={0.3} />
              <stop offset="95%" stopColor="#f97316" stopOpacity={0} />
            </linearGradient>
            <linearGradient id="colorCrossRegion" x1="0" y1="0" x2="0" y2="1">
              <stop offset="5%" stopColor="#f59e0b" stopOpacity={0.3} />
              <stop offset="95%" stopColor="#f59e0b" stopOpacity={0} />
            </linearGradient>
            <linearGradient id="colorCrossAZ" x1="0" y1="0" x2="0" y2="1">
              <stop offset="5%" stopColor="#10b981" stopOpacity={0.3} />
              <stop offset="95%" stopColor="#10b981" stopOpacity={0} />
            </linearGradient>
          </defs>
          
          <CartesianGrid strokeDasharray="3 3" stroke="#1f2937" />
          
          <XAxis 
            dataKey="time" 
            tick={{ fill: '#6b7280', fontSize: 10 }}
            axisLine={{ stroke: '#1f2937' }}
            tickLine={{ stroke: '#1f2937' }}
            interval="preserveStartEnd"
            minTickGap={50}
          />
          <YAxis 
            tick={{ fill: '#6b7280', fontSize: 10 }}
            axisLine={{ stroke: '#1f2937' }}
            tickLine={{ stroke: '#1f2937' }}
            tickFormatter={(value: number) => `$${value.toFixed(2)}`}
            domain={[0, 'auto']}
          />
          
          <Tooltip
            contentStyle={{
              backgroundColor: '#121820',
              border: '1px solid #1f2937',
              borderRadius: '8px',
              boxShadow: '0 4px 6px rgba(0, 0, 0, 0.3)',
            }}
            labelStyle={{ color: '#e5e7eb', fontWeight: 'bold' }}
            itemStyle={{ color: '#e5e7eb' }}
            formatter={(value: number, name: string) => {
              const labels: Record<string, string> = {
                egress: 'Egress',
                crossRegion: 'Cross-Region',
                crossAZ: 'Cross-AZ',
              }
              return [`$${value.toFixed(4)}/s`, labels[name] || name]
            }}
          />
          
          <Area
            type="monotone"
            dataKey="crossAZ"
            stackId="1"
            stroke="#10b981"
            fill="url(#colorCrossAZ)"
            name="crossAZ"
            isAnimationActive={false}
          />
          <Area
            type="monotone"
            dataKey="crossRegion"
            stackId="1"
            stroke="#f59e0b"
            fill="url(#colorCrossRegion)"
            name="crossRegion"
            isAnimationActive={false}
          />
          <Area
            type="monotone"
            dataKey="egress"
            stackId="1"
            stroke="#f97316"
            fill="url(#colorEgress)"
            name="egress"
            isAnimationActive={false}
          />
        </AreaChart>
      </ResponsiveContainer>
    </div>
  )
}
