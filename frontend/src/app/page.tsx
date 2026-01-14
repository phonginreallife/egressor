'use client'

import { useState } from 'react'
import { motion } from 'framer-motion'
import { 
  Activity, 
  DollarSign, 
  AlertTriangle, 
  Network, 
  ArrowUpRight,
  ArrowDownLeft,
  Zap,
  MessageSquare,
  TrendingUp,
  Server
} from 'lucide-react'
import { useQuery } from '@tanstack/react-query'
import { CostChart } from '@/components/CostChart'
import { TransferGraph } from '@/components/TransferGraph'
import { AnomalyList } from '@/components/AnomalyList'
import { ClaudeChat } from '@/components/ClaudeChat'
import { StatsCard } from '@/components/StatsCard'
import { TopFlows } from '@/components/TopFlows'

async function fetchStats() {
  const res = await fetch('/api/v1/graph/stats')
  if (!res.ok) throw new Error('Failed to fetch stats')
  return res.json()
}

async function fetchCostSummary() {
  const res = await fetch('/api/v1/costs/summary')
  if (!res.ok) throw new Error('Failed to fetch costs')
  return res.json()
}

async function fetchAnomalies() {
  const res = await fetch('/api/v1/anomalies/summary')
  if (!res.ok) throw new Error('Failed to fetch anomalies')
  return res.json()
}

export default function Dashboard() {
  const [activeTab, setActiveTab] = useState<'overview' | 'graph' | 'costs' | 'anomalies' | 'claude'>('overview')
  
  const { data: stats } = useQuery({ 
    queryKey: ['stats'], 
    queryFn: fetchStats,
    refetchInterval: 2000, // Real-time updates every 2s
  })
  const { data: costs } = useQuery({ 
    queryKey: ['costs'], 
    queryFn: fetchCostSummary,
    refetchInterval: 2000,
  })
  const { data: anomalies } = useQuery({ 
    queryKey: ['anomalies'], 
    queryFn: fetchAnomalies,
    refetchInterval: 2000,
  })

  const tabs = [
    { id: 'overview', label: 'Overview', icon: Activity },
    { id: 'graph', label: 'Transfer Graph', icon: Network },
    { id: 'costs', label: 'Costs', icon: DollarSign },
    { id: 'anomalies', label: 'Anomalies', icon: AlertTriangle },
    { id: 'claude', label: 'Ask Claude', icon: MessageSquare },
  ]

  return (
    <div className="min-h-screen grid-pattern">
      {/* Header */}
      <header className="border-b border-flow-border bg-flow-surface/80 backdrop-blur-sm sticky top-0 z-50">
        <div className="container mx-auto px-6 py-4">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-3">
              <div className="w-10 h-10 rounded-lg bg-gradient-to-br from-flow-accent to-emerald-600 flex items-center justify-center">
                <Zap className="w-6 h-6 text-white" />
              </div>
              <div>
                <h1 className="text-xl font-bold text-flow-text">Egressor</h1>
                <p className="text-xs text-flow-muted">Data Transfer Intelligence</p>
              </div>
            </div>
            
            <nav className="flex gap-1">
              {tabs.map((tab) => (
                <button
                  key={tab.id}
                  onClick={() => setActiveTab(tab.id as any)}
                  className={`flex items-center gap-2 px-4 py-2 rounded-lg transition-all ${
                    activeTab === tab.id 
                      ? 'bg-flow-accent/10 text-flow-accent border border-flow-accent/30' 
                      : 'text-flow-muted hover:text-flow-text hover:bg-flow-surface'
                  }`}
                >
                  <tab.icon className="w-4 h-4" />
                  <span className="text-sm">{tab.label}</span>
                </button>
              ))}
            </nav>
          </div>
        </div>
      </header>

      {/* Main Content */}
      <main className="container mx-auto px-6 py-8">
        {activeTab === 'overview' && (
          <motion.div
            initial={{ opacity: 0, y: 20 }}
            animate={{ opacity: 1, y: 0 }}
            className="space-y-8"
          >
            {/* Stats Grid */}
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-6">
              <StatsCard
                title="Total Transfer"
                value={formatBytes(stats?.total_bytes || 0)}
                subtitle="Last 24 hours"
                icon={Activity}
                trend={+12}
                color="accent"
              />
              <StatsCard
                title="Egress Cost"
                value={`$${(costs?.egress_cost_usd || 0).toFixed(2)}`}
                subtitle="Estimated"
                icon={ArrowUpRight}
                trend={-5}
                color="egress"
              />
              <StatsCard
                title="Active Services"
                value={stats?.total_nodes || 0}
                subtitle="In cluster"
                icon={Server}
                color="info"
              />
              <StatsCard
                title="Active Anomalies"
                value={anomalies?.total_active || 0}
                subtitle={`${anomalies?.by_severity?.high || 0} high severity`}
                icon={AlertTriangle}
                trend={anomalies?.total_active > 0 ? undefined : 0}
                color={anomalies?.total_active > 0 ? 'danger' : 'accent'}
              />
            </div>

            {/* Charts Row */}
            <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
              <div className="stat-card">
                <h3 className="text-lg font-semibold mb-4 flex items-center gap-2">
                  <TrendingUp className="w-5 h-5 text-flow-accent" />
                  Transfer Trends
                </h3>
                <CostChart />
              </div>
              
              <div className="stat-card">
                <h3 className="text-lg font-semibold mb-4 flex items-center gap-2">
                  <Network className="w-5 h-5 text-flow-accent" />
                  Top Flows
                </h3>
                <TopFlows />
              </div>
            </div>

            {/* Bottom Section */}
            <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
              <div className="lg:col-span-2 stat-card">
                <h3 className="text-lg font-semibold mb-4 flex items-center gap-2">
                  <AlertTriangle className="w-5 h-5 text-flow-warning" />
                  Recent Anomalies
                </h3>
                <AnomalyList limit={5} />
              </div>
              
              <div className="stat-card">
                <h3 className="text-lg font-semibold mb-4 flex items-center gap-2">
                  <DollarSign className="w-5 h-5 text-flow-egress" />
                  Cost Breakdown
                </h3>
                <div className="space-y-4">
                  <CostItem label="Internet Egress" value={costs?.egress_cost_usd || 0} total={costs?.total_cost_usd || 1} color="egress" />
                  <CostItem label="Cross-Region" value={costs?.cross_region_cost_usd || 0} total={costs?.total_cost_usd || 1} color="warning" />
                  <CostItem label="Cross-AZ" value={costs?.cross_az_cost_usd || 0} total={costs?.total_cost_usd || 1} color="accent" />
                </div>
              </div>
            </div>
          </motion.div>
        )}

        {activeTab === 'graph' && (
          <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            className="stat-card min-h-[600px]"
          >
            <TransferGraph />
          </motion.div>
        )}

        {activeTab === 'costs' && (
          <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            className="space-y-6"
          >
            <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
              <StatsCard
                title="Total Cost"
                value={`$${(costs?.total_cost_usd || 0).toFixed(2)}`}
                subtitle="This month"
                icon={DollarSign}
                color="accent"
              />
              <StatsCard
                title="Egress"
                value={`$${(costs?.egress_cost_usd || 0).toFixed(2)}`}
                subtitle="Internet bound"
                icon={ArrowUpRight}
                color="egress"
              />
              <StatsCard
                title="Cross-Region"
                value={`$${(costs?.cross_region_cost_usd || 0).toFixed(2)}`}
                subtitle="Between regions"
                icon={Network}
                color="warning"
              />
            </div>
            <div className="stat-card">
              <h3 className="text-lg font-semibold mb-4">Cost Over Time</h3>
              <CostChart height={400} />
            </div>
          </motion.div>
        )}

        {activeTab === 'anomalies' && (
          <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            className="space-y-6"
          >
            <div className="grid grid-cols-1 md:grid-cols-4 gap-6">
              <StatsCard
                title="Active"
                value={anomalies?.total_active || 0}
                icon={AlertTriangle}
                color="danger"
              />
              <StatsCard
                title="Critical"
                value={anomalies?.by_severity?.critical || 0}
                icon={Zap}
                color="danger"
              />
              <StatsCard
                title="High"
                value={anomalies?.by_severity?.high || 0}
                icon={AlertTriangle}
                color="warning"
              />
              <StatsCard
                title="Cost Impact"
                value={`$${(anomalies?.total_cost_impact_usd || 0).toFixed(2)}`}
                icon={DollarSign}
                color="egress"
              />
            </div>
            <div className="stat-card">
              <h3 className="text-lg font-semibold mb-4">All Anomalies</h3>
              <AnomalyList />
            </div>
          </motion.div>
        )}

        {activeTab === 'claude' && (
          <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            className="stat-card min-h-[600px]"
          >
            <ClaudeChat />
          </motion.div>
        )}
      </main>
    </div>
  )
}

function CostItem({ label, value, total, color }: { label: string; value: number; total: number; color: string }) {
  const percent = total > 0 ? (value / total) * 100 : 0
  const colorClasses: Record<string, string> = {
    egress: 'bg-flow-egress',
    warning: 'bg-flow-warning',
    accent: 'bg-flow-accent',
  }
  
  return (
    <div className="space-y-2">
      <div className="flex justify-between text-sm">
        <span className="text-flow-muted">{label}</span>
        <span className="text-flow-text">${value.toFixed(2)}</span>
      </div>
      <div className="h-2 bg-flow-border rounded-full overflow-hidden">
        <div 
          className={`h-full ${colorClasses[color]} transition-all duration-500`}
          style={{ width: `${percent}%` }}
        />
      </div>
    </div>
  )
}

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return `${parseFloat((bytes / Math.pow(k, i)).toFixed(2))} ${sizes[i]}`
}
