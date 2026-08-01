import { useState } from 'react'
import { api } from '../../api'
import { usePoll } from '../../hooks/usePoll'

// Physical ownership tree: modules are flat labels (not necessarily nested
// paths), so this renders a sorted list of modules by file count — clicking
// one loads its files inline via the same filtered entries endpoint the All
// Files view uses.
export function RegistryArchitectureView({
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
  const { data: stats, error } = usePoll(() => api.registryStats(projectId), 15000, onUnauthorized, [projectId, tick])
  const [selected, setSelected] = useState<string>()

  const { data: filesPage } = usePoll(
    () => (selected ? api.registryEntries(projectId, { module: selected, limit: 200 }) : Promise.resolve(undefined)),
    15000,
    onUnauthorized,
    [projectId, selected, tick],
  )

  const modules = stats ? Object.entries(stats.by_module).sort((a, b) => b[1] - a[1]) : []

  return (
    <>
      <div className="view-title">Arquitetura</div>
      {error && <div className="error-banner">{error}</div>}
      <div className="registry-split">
        <div className="registry-split-side">
          {modules.map(([module, count]) => (
            <div
              className={`registry-tree-row ${selected === module ? 'active' : ''}`}
              key={module}
              onClick={() => setSelected(module)}
            >
              <span>{module || '(sem módulo)'}</span>
              <span className="card-subtitle">{count}</span>
            </div>
          ))}
        </div>
        <div className="registry-split-main">
          {!selected && <div className="empty-state">selecione um módulo para ver seus arquivos.</div>}
          {selected && (
            <>
              <div className="card-title">{selected}</div>
              {filesPage?.results.map((r) => (
                <div className="card-row" key={r.entry.entry_id}>
                  <button type="button" className="btn-link" onClick={() => onOpenEntry(r.entry.entry_id)}>
                    {r.entry.path}
                  </button>
                </div>
              ))}
            </>
          )}
        </div>
      </div>
    </>
  )
}
