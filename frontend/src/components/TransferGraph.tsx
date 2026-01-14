'use client'

import { useEffect, useRef, useState, useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'

interface Node {
  id: string
  namespace: string
  name: string
  total_bytes_sent: number
  total_bytes_received: number
}

interface Edge {
  source: string
  target: string
  transfer_type: string
  total_bytes: number
  cost_usd: number
}

interface GraphData {
  nodes: Node[]
  edges: Edge[]
}

async function fetchGraph(): Promise<GraphData> {
  const res = await fetch('/api/v1/graph')
  if (!res.ok) throw new Error('Failed to fetch graph')
  const data = await res.json()
  return {
    nodes: data.nodes || [],
    edges: data.edges || [],
  }
}

export function TransferGraph() {
  const { data: graph, isLoading, error } = useQuery({
    queryKey: ['graph'],
    queryFn: fetchGraph,
    refetchInterval: 2000, // Real-time updates every 2 seconds
    staleTime: 1000,
  })

  const svgRef = useRef<SVGSVGElement>(null)
  const [dimensions, setDimensions] = useState({ width: 800, height: 500 })
  const [hoveredNode, setHoveredNode] = useState<string | null>(null)

  useEffect(() => {
    const updateDimensions = () => {
      if (svgRef.current?.parentElement) {
        const { width, height } = svgRef.current.parentElement.getBoundingClientRect()
        setDimensions({ width: width - 40, height: Math.max(height - 80, 400) })
      }
    }

    updateDimensions()
    window.addEventListener('resize', updateDimensions)
    return () => window.removeEventListener('resize', updateDimensions)
  }, [])

  // Process and simplify the graph - limit external nodes, aggregate small ones
  const processedGraph = useMemo(() => {
    if (!graph) return null

    const MAX_EXTERNAL_NODES = 8
    const MAX_INTERNAL_NODES = 20

    // Separate nodes
    const internalNodes = graph.nodes.filter(n => !n.id.startsWith('external:'))
    const externalNodes = graph.nodes.filter(n => n.id.startsWith('external:'))

    // Sort by traffic and take top N
    const sortedInternal = [...internalNodes]
      .sort((a, b) => (b.total_bytes_sent + b.total_bytes_received) - (a.total_bytes_sent + a.total_bytes_received))
      .slice(0, MAX_INTERNAL_NODES)

    const sortedExternal = [...externalNodes]
      .sort((a, b) => (b.total_bytes_sent + b.total_bytes_received) - (a.total_bytes_sent + a.total_bytes_received))
      .slice(0, MAX_EXTERNAL_NODES)

    // Create "Other External" aggregate node if we have too many
    const otherExternalBytes = externalNodes
      .slice(MAX_EXTERNAL_NODES)
      .reduce((sum, n) => sum + n.total_bytes_sent + n.total_bytes_received, 0)

    if (otherExternalBytes > 0 && externalNodes.length > MAX_EXTERNAL_NODES) {
      sortedExternal.push({
        id: 'external:other',
        namespace: 'external',
        name: `+${externalNodes.length - MAX_EXTERNAL_NODES} others`,
        total_bytes_sent: otherExternalBytes,
        total_bytes_received: 0,
      })
    }

    const visibleNodes = [...sortedInternal, ...sortedExternal]
    const visibleNodeIds = new Set(visibleNodes.map(n => n.id))

    // Filter edges to only include visible nodes, aggregate others to "other"
    const filteredEdges = graph.edges
      .filter(e => visibleNodeIds.has(e.source) || e.source.startsWith('external:'))
      .map(e => ({
        ...e,
        target: visibleNodeIds.has(e.target) ? e.target : 
                e.target.startsWith('external:') ? 'external:other' : e.target
      }))
      .filter(e => visibleNodeIds.has(e.source) && visibleNodeIds.has(e.target))

    // Aggregate duplicate edges
    const edgeMap = new Map<string, Edge>()
    filteredEdges.forEach(edge => {
      const key = `${edge.source}->${edge.target}`
      const existing = edgeMap.get(key)
      if (existing) {
        existing.total_bytes += edge.total_bytes
        existing.cost_usd += edge.cost_usd
      } else {
        edgeMap.set(key, { ...edge })
      }
    })

    return {
      nodes: visibleNodes,
      edges: Array.from(edgeMap.values()),
      totalNodes: graph.nodes.length,
      totalEdges: graph.edges.length,
    }
  }, [graph])

  if (isLoading || !processedGraph) {
    return (
      <div className="flex items-center justify-center h-[500px] text-flow-muted">
        <div className="flex items-center gap-2">
          <div className="w-4 h-4 border-2 border-emerald-500 border-t-transparent rounded-full animate-spin" />
          Loading graph...
        </div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="flex items-center justify-center h-[500px] text-red-400">
        Failed to load graph
      </div>
    )
  }

  // Calculate positions
  const nodePositions = calculateNodePositions(processedGraph.nodes, dimensions)

  const edgeColors: Record<string, string> = {
    egress: '#f97316',
    cross_region: '#ef4444',
    cross_az: '#f59e0b',
    pod_to_pod: '#10b981',
    internal: '#10b981',
  }

  // Find connected edges for hover effect
  const connectedEdges = hoveredNode
    ? new Set(processedGraph.edges
        .filter(e => e.source === hoveredNode || e.target === hoveredNode)
        .flatMap(e => [e.source, e.target]))
    : null

  return (
    <div className="relative">
      {/* Legend */}
      <div className="absolute top-4 left-4 flex gap-4 z-10">
        <Legend color="#10b981" label="Internal" />
        <Legend color="#f59e0b" label="Cross-AZ" />
        <Legend color="#f97316" label="Egress" />
        <Legend color="#ef4444" label="Cross-Region" />
      </div>

      {/* Stats */}
      <div className="absolute top-4 right-4 text-xs text-flow-muted z-10 bg-flow-card/80 px-2 py-1 rounded">
        <span className="text-emerald-400">{processedGraph.nodes.length}</span> nodes / 
        <span className="text-emerald-400 ml-1">{processedGraph.edges.length}</span> edges
        {processedGraph.totalNodes > processedGraph.nodes.length && (
          <span className="ml-1 text-amber-400">
            (showing top {processedGraph.nodes.length} of {processedGraph.totalNodes})
          </span>
        )}
      </div>

      <svg
        ref={svgRef}
        width={dimensions.width}
        height={dimensions.height}
        className="overflow-visible"
      >
        <defs>
          {Object.entries(edgeColors).map(([type, color]) => (
            <marker
              key={type}
              id={`arrowhead-${type}`}
              markerWidth="8"
              markerHeight="6"
              refX="8"
              refY="3"
              orient="auto"
            >
              <polygon points="0 0, 8 3, 0 6" fill={color} />
            </marker>
          ))}
          <filter id="glow">
            <feGaussianBlur stdDeviation="3" result="coloredBlur"/>
            <feMerge>
              <feMergeNode in="coloredBlur"/>
              <feMergeNode in="SourceGraphic"/>
            </feMerge>
          </filter>
        </defs>

        {/* Edges */}
        <g>
          {processedGraph.edges.map((edge, i) => {
            const source = nodePositions[edge.source]
            const target = nodePositions[edge.target]
            if (!source || !target) return null

            const color = edgeColors[edge.transfer_type] || edgeColors.internal
            const strokeWidth = Math.max(1, Math.min(4, Math.log10(edge.total_bytes / 1000000000) + 1))
            const isHighlighted = connectedEdges?.has(edge.source) && connectedEdges?.has(edge.target)
            const isDimmed = hoveredNode && !isHighlighted

            // Calculate curved path for better visibility
            const dx = target.x - source.x
            const dy = target.y - source.y
            const dr = Math.sqrt(dx * dx + dy * dy) * 0.5

            return (
              <g key={`${edge.source}-${edge.target}-${i}`}>
                <path
                  d={`M${source.x},${source.y} Q${(source.x + target.x) / 2 + dy * 0.1},${(source.y + target.y) / 2 - dx * 0.1} ${target.x},${target.y}`}
                  fill="none"
                  stroke={color}
                  strokeWidth={isHighlighted ? strokeWidth + 1 : strokeWidth}
                  strokeOpacity={isDimmed ? 0.15 : isHighlighted ? 0.9 : 0.5}
                  markerEnd={`url(#arrowhead-${edge.transfer_type || 'internal'})`}
                  className="transition-all duration-200"
                />
              </g>
            )
          })}
        </g>

        {/* Nodes */}
        <g>
          {processedGraph.nodes.map((node) => {
            const pos = nodePositions[node.id]
            if (!pos) return null

            const isExternal = node.id.startsWith('external:')
            const totalBytes = node.total_bytes_sent + node.total_bytes_received
            const radius = Math.max(12, Math.min(30, Math.log10(totalBytes / 1000000 + 1) * 8))
            const isHovered = hoveredNode === node.id
            const isConnected = connectedEdges?.has(node.id)
            const isDimmed = hoveredNode && !isConnected && !isHovered

            const nodeColor = isExternal ? '#f97316' : '#10b981'

            return (
              <g 
                key={node.id} 
                className="cursor-pointer"
                onMouseEnter={() => setHoveredNode(node.id)}
                onMouseLeave={() => setHoveredNode(null)}
              >
                <circle
                  cx={pos.x}
                  cy={pos.y}
                  r={isHovered ? radius + 4 : radius}
                  fill={nodeColor}
                  fillOpacity={isDimmed ? 0.1 : isHovered ? 0.4 : 0.25}
                  stroke={nodeColor}
                  strokeWidth={isHovered ? 3 : 2}
                  strokeOpacity={isDimmed ? 0.3 : 1}
                  filter={isHovered ? 'url(#glow)' : undefined}
                  className="transition-all duration-200"
                />
                <text
                  x={pos.x}
                  y={pos.y + radius + 14}
                  fill={isDimmed ? '#4b5563' : '#e5e7eb'}
                  fontSize="10"
                  fontWeight={isHovered ? 'bold' : 'normal'}
                  textAnchor="middle"
                  className="transition-all duration-200 pointer-events-none"
                >
                  {node.name.length > 20 ? node.name.slice(0, 18) + '...' : node.name}
                </text>
                {!isExternal && (
                  <text
                    x={pos.x}
                    y={pos.y + radius + 25}
                    fill={isDimmed ? '#374151' : '#6b7280'}
                    fontSize="8"
                    textAnchor="middle"
                    className="pointer-events-none"
                  >
                    {node.namespace}
                  </text>
                )}
                {/* Tooltip on hover */}
                {isHovered && (
                  <g>
                    <rect
                      x={pos.x - 60}
                      y={pos.y - radius - 45}
                      width={120}
                      height={35}
                      fill="#1f2937"
                      stroke="#374151"
                      rx={4}
                    />
                    <text
                      x={pos.x}
                      y={pos.y - radius - 30}
                      fill="#e5e7eb"
                      fontSize="9"
                      textAnchor="middle"
                    >
                      {formatBytes(totalBytes)}
                    </text>
                    <text
                      x={pos.x}
                      y={pos.y - radius - 18}
                      fill="#6b7280"
                      fontSize="8"
                      textAnchor="middle"
                    >
                      {isExternal ? 'External' : node.namespace}
                    </text>
                  </g>
                )}
              </g>
            )
          })}
        </g>
      </svg>
    </div>
  )
}

function calculateNodePositions(
  nodes: Node[],
  dimensions: { width: number; height: number }
): Record<string, { x: number; y: number }> {
  const positions: Record<string, { x: number; y: number }> = {}
  const centerX = dimensions.width / 2
  const centerY = dimensions.height / 2 + 20 // Offset for legend

  // Separate internal and external nodes
  const internalNodes = nodes.filter(n => !n.id.startsWith('external:'))
  const externalNodes = nodes.filter(n => n.id.startsWith('external:'))

  // Position internal nodes in concentric circles based on namespace
  const namespaces = Array.from(new Set(internalNodes.map(n => n.namespace)))
  const namespaceGroups: Record<string, Node[]> = {}
  internalNodes.forEach(n => {
    if (!namespaceGroups[n.namespace]) namespaceGroups[n.namespace] = []
    namespaceGroups[n.namespace].push(n)
  })

  // Smaller radius for cleaner layout
  const baseRadius = Math.min(dimensions.width, dimensions.height) * 0.25

  let totalInternalPlaced = 0
  namespaces.forEach((ns, nsIndex) => {
    const group = namespaceGroups[ns]
    const ringRadius = baseRadius + nsIndex * 50

    group.forEach((node, i) => {
      const angleOffset = nsIndex * 0.3 // Offset rings for visual separation
      const angle = (2 * Math.PI * i) / group.length - Math.PI / 2 + angleOffset
      positions[node.id] = {
        x: centerX + ringRadius * Math.cos(angle),
        y: centerY + ringRadius * Math.sin(angle),
      }
      totalInternalPlaced++
    })
  })

  // Position external nodes on the right side in a column
  const externalStartY = 80
  const externalSpacing = Math.min(60, (dimensions.height - 160) / Math.max(externalNodes.length, 1))
  const externalX = dimensions.width - 100

  externalNodes.forEach((node, i) => {
    positions[node.id] = {
      x: externalX,
      y: externalStartY + i * externalSpacing + externalSpacing / 2,
    }
  })

  return positions
}

function formatBytes(bytes: number): string {
  if (bytes >= 1e12) return `${(bytes / 1e12).toFixed(1)} TB`
  if (bytes >= 1e9) return `${(bytes / 1e9).toFixed(1)} GB`
  if (bytes >= 1e6) return `${(bytes / 1e6).toFixed(1)} MB`
  if (bytes >= 1e3) return `${(bytes / 1e3).toFixed(1)} KB`
  return `${bytes} B`
}

function Legend({ color, label }: { color: string; label: string }) {
  return (
    <div className="flex items-center gap-2 text-xs">
      <div className="w-3 h-3 rounded-full" style={{ backgroundColor: color }} />
      <span className="text-flow-muted">{label}</span>
    </div>
  )
}
