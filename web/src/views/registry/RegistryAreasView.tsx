import { useState } from 'react'
import { api } from '../../api'
import { usePoll } from '../../hooks/usePoll'
import type { RegistryAreaNode } from '../../types'

function AreaTree({
  nodes,
  counts,
  selected,
  onSelect,
  depth,
}: {
  nodes: RegistryAreaNode[]
  counts: Record<string, number>
  selected?: string
  onSelect: (id: string) => void
  depth: number
}) {
  return (
    <>
      {nodes.map((node) => (
        <div key={node.id}>
          <div
            className={`registry-tree-row ${selected === node.id ? 'active' : ''}`}
            style={{ paddingLeft: `${depth * 1.25}rem` }}
            onClick={() => onSelect(node.id)}
          >
            <span>{node.label}</span>
            <span className="card-subtitle">{counts[node.id] ?? 0}</span>
          </div>
          {node.children && node.children.length > 0 && (
            <AreaTree nodes={node.children} counts={counts} selected={selected} onSelect={onSelect} depth={depth + 1} />
          )}
        </div>
      ))}
    </>
  )
}

export function RegistryAreasView({
  projectId,
  tick,
  onUnauthorized,
  onOpenEntry,
}: {
  projectId: string
  tick: number
  onUnauthorized: () => void
  onOpenEntry: (entryId: string) => void
}) {
  const { data: taxonomy, error: taxError } = usePoll(() => api.registryTaxonomy(projectId), 30000, onUnauthorized, [projectId, tick])
  const { data: stats, error: statsError } = usePoll(() => api.registryStats(projectId), 15000, onUnauthorized, [projectId, tick])
  const [selected, setSelected] = useState<string>()

  const { data: filesPage } = usePoll(
    () => (selected ? api.registryEntries(projectId, { area: selected, limit: 200 }) : Promise.resolve(undefined)),
    15000,
    onUnauthorized,
    [projectId, selected, tick],
  )

  return (
    <>
      <div className="view-title">Product Areas</div>
      {taxError && <div className="error-banner">{taxError}</div>}
      {statsError && <div className="error-banner">{statsError}</div>}
      <div className="registry-split">
        <div className="registry-split-side">
          {taxonomy && (
            <AreaTree nodes={taxonomy.areas} counts={stats?.by_area ?? {}} selected={selected} onSelect={setSelected} depth={0} />
          )}
        </div>
        <div className="registry-split-main">
          {!selected && <div className="empty-state">selecione uma área para ver seus arquivos.</div>}
          {selected &&
            (filesPage?.results.length ? (
              filesPage.results.map((r) => (
                <div className="card-row" key={r.entry.entry_id}>
                  <button type="button" className="btn-link" onClick={() => onOpenEntry(r.entry.entry_id)}>
                    {r.entry.path}
                  </button>
                </div>
              ))
            ) : (
              <div className="empty-state">nenhum arquivo nesta área ainda.</div>
            ))}
        </div>
      </div>
    </>
  )
}
