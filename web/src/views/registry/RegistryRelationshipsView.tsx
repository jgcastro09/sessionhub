import { useMemo, useState } from 'react'
import { api } from '../../api'
import { usePoll } from '../../hooks/usePoll'
import type { RegistryGraph, RegistryGraphNode } from '../../types'

const EDGE_COLOR: Record<string, string> = {
  contains: 'var(--text-dim)',
  implements: '#7aa2f7',
  imports: '#9ece6a',
  includes: '#9ece6a',
  related: '#e0af68',
  probable: '#565f89',
}

const NODE_COLOR: Record<string, string> = {
  file: '#7aa2f7',
  area: '#e0af68',
  module: '#bb9af7',
}

interface Positioned {
  node: RegistryGraphNode
  x: number
  y: number
}

// A deliberately dependency-free relationship graph: no charting/graph
// library, just plain SVG with a circular layout (seed node centered, every
// other node placed evenly on a ring around it). Good enough to see fan-out
// and click through — a force-directed layout can replace this later
// without touching any other view, since RegistryGraph is already the
// shared data shape.
function layout(graph: RegistryGraph, seedId: string | undefined, width: number, height: number): Positioned[] {
  const cx = width / 2
  const cy = height / 2
  const seed = seedId ? graph.nodes.find((n) => n.id === seedId) : undefined
  const others = graph.nodes.filter((n) => n.id !== seed?.id)
  const radius = Math.min(width, height) / 2 - 60
  const positions: Positioned[] = []
  if (seed) positions.push({ node: seed, x: cx, y: cy })
  others.forEach((node, i) => {
    const angle = (2 * Math.PI * i) / Math.max(1, others.length)
    positions.push({ node, x: cx + radius * Math.cos(angle), y: cy + radius * Math.sin(angle) })
  })
  return positions
}

export function RegistryRelationshipsView({
  projectId,
  tick,
  onUnauthorized,
  onOpenEntry,
  initialSeedEntryId,
}: {
  projectId: string
  tick: number
  onUnauthorized: () => void
  onOpenEntry: (entryId: string) => void
  initialSeedEntryId?: string
}) {
  const [seedEntryId, setSeedEntryId] = useState(initialSeedEntryId ?? '')
  const [depth, setDepth] = useState(2)
  const [limit, setLimit] = useState(60)

  const { data, error } = usePoll(
    () =>
      seedEntryId
        ? api.registryEntryGraph(projectId, seedEntryId, depth, limit)
        : api.registryGraph(projectId).then((g) => ({ nodes: g.nodes.slice(0, limit), edges: g.edges })),
    20000,
    onUnauthorized,
    [projectId, tick, seedEntryId, depth, limit],
  )

  const width = 900
  const height = 560
  const positions = useMemo(() => (data ? layout(data, seedEntryId || undefined, width, height) : []), [data, seedEntryId])
  const byId = useMemo(() => new Map(positions.map((p) => [p.node.id, p])), [positions])

  return (
    <>
      <div className="view-title">Relacionamentos</div>
      <div className="registry-filter-bar">
        <input
          className="text-input"
          placeholder="entry_id semente (vazio = grafo inteiro)"
          value={seedEntryId}
          onChange={(e) => setSeedEntryId(e.target.value.trim())}
        />
        <label className="card-subtitle">
          profundidade{' '}
          <input
            className="text-input registry-number-input"
            type="number"
            min={1}
            max={6}
            value={depth}
            onChange={(e) => setDepth(Number(e.target.value) || 2)}
          />
        </label>
        <label className="card-subtitle">
          limite{' '}
          <input
            className="text-input registry-number-input"
            type="number"
            min={5}
            max={300}
            value={limit}
            onChange={(e) => setLimit(Number(e.target.value) || 60)}
          />
        </label>
      </div>
      {error && <div className="error-banner">{error}</div>}
      {data && data.nodes.length === 0 && <div className="empty-state">nenhuma relação para mostrar ainda.</div>}
      {data && data.nodes.length > 0 && (
        <>
          <svg width="100%" viewBox={`0 0 ${width} ${height}`} className="registry-graph-svg">
            {data.edges.map((edge, i) => {
              const from = byId.get(edge.from)
              const to = byId.get(edge.to)
              if (!from || !to) return null
              return (
                <line
                  key={i}
                  x1={from.x}
                  y1={from.y}
                  x2={to.x}
                  y2={to.y}
                  stroke={EDGE_COLOR[edge.kind] ?? '#888'}
                  strokeWidth={edge.confidence === 'probable' || edge.confidence === 'ambiguous' ? 1 : 1.5}
                  strokeDasharray={edge.confidence === 'probable' ? '4 3' : undefined}
                  opacity={0.7}
                />
              )
            })}
            {positions.map((p) => (
              <g
                key={p.node.id}
                transform={`translate(${p.x},${p.y})`}
                className="registry-graph-node"
                onClick={() => p.node.kind === 'file' && onOpenEntry(p.node.id)}
              >
                <circle r={p.node.id === seedEntryId ? 10 : 7} fill={NODE_COLOR[p.node.kind] ?? '#888'} />
                <text x={12} y={4} className="registry-graph-label">
                  {p.node.label}
                </text>
              </g>
            ))}
          </svg>
          <div className="registry-graph-legend">
            <span>
              <i style={{ background: NODE_COLOR.file }} /> arquivo
            </span>
            <span>
              <i style={{ background: NODE_COLOR.module }} /> módulo
            </span>
            <span>
              <i style={{ background: NODE_COLOR.area }} /> área
            </span>
            <span className="card-subtitle">{data.nodes.length} nós · {data.edges.length} arestas</span>
          </div>
        </>
      )}
    </>
  )
}
