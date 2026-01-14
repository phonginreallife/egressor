'use client'

import { motion } from 'framer-motion'
import { LucideIcon, TrendingUp, TrendingDown } from 'lucide-react'

interface StatsCardProps {
  title: string
  value: string | number
  subtitle?: string
  icon: LucideIcon
  trend?: number
  color?: 'accent' | 'egress' | 'warning' | 'danger' | 'info'
}

const colorMap = {
  accent: 'text-flow-accent border-flow-accent/30 bg-flow-accent/5',
  egress: 'text-flow-egress border-flow-egress/30 bg-flow-egress/5',
  warning: 'text-flow-warning border-flow-warning/30 bg-flow-warning/5',
  danger: 'text-flow-danger border-flow-danger/30 bg-flow-danger/5',
  info: 'text-flow-info border-flow-info/30 bg-flow-info/5',
}

const iconColorMap = {
  accent: 'text-flow-accent',
  egress: 'text-flow-egress',
  warning: 'text-flow-warning',
  danger: 'text-flow-danger',
  info: 'text-flow-info',
}

export function StatsCard({ title, value, subtitle, icon: Icon, trend, color = 'accent' }: StatsCardProps) {
  return (
    <motion.div
      initial={{ opacity: 0, y: 20 }}
      animate={{ opacity: 1, y: 0 }}
      className={`stat-card border ${colorMap[color]}`}
    >
      <div className="flex items-start justify-between mb-4">
        <div className={`p-2 rounded-lg ${colorMap[color]}`}>
          <Icon className={`w-5 h-5 ${iconColorMap[color]}`} />
        </div>
        {trend !== undefined && (
          <div className={`flex items-center gap-1 text-sm ${
            trend > 0 ? 'text-flow-danger' : trend < 0 ? 'text-flow-accent' : 'text-flow-muted'
          }`}>
            {trend > 0 ? <TrendingUp className="w-4 h-4" /> : trend < 0 ? <TrendingDown className="w-4 h-4" /> : null}
            <span>{Math.abs(trend)}%</span>
          </div>
        )}
      </div>
      
      <div className="space-y-1">
        <p className="text-sm text-flow-muted">{title}</p>
        <p className="text-2xl font-bold text-flow-text">{value}</p>
        {subtitle && <p className="text-xs text-flow-muted">{subtitle}</p>}
      </div>
    </motion.div>
  )
}
