import { useState } from 'react'
import { api } from '../../api'
import { usePoll } from '../../hooks/usePoll'

const PAGE_SIZE = 25

export function RegistryReviewQueueView({
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
  const [page, setPage] = useState(0)
  const { data, error, loading } = usePoll(
    () => api.registryReviewQueue(projectId, PAGE_SIZE, page * PAGE_SIZE),
    15000,
    onUnauthorized,
    [projectId, tick, page],
  )

  const results = data?.results ?? []
  const total = data?.total ?? 0

  return (
    <>
      <div className="view-title">Fila de revisão</div>
      {error && <div className="error-banner">{error}</div>}
      {!loading && results.length === 0 && (
        <div className="empty-state">
          <div className="prompt">$</div>
          Nada pendente de revisão.
        </div>
      )}
      <div className="card-list">
        {results.map((r) => (
          <div className="card" key={r.entry.entry_id} onClick={() => onOpenEntry(r.entry.entry_id)} style={{ cursor: 'pointer' }}>
            <div className="card-row">
              <div className="card-title">{r.entry.path}</div>
            </div>
            <div className="card-subtitle">
              {r.entry.language} · {r.entry.category} · atualizado em {new Date(r.entry.updated_at).toLocaleString()}
            </div>
          </div>
        ))}
      </div>
      <div className="registry-pagination">
        <button type="button" className="btn-link" disabled={page === 0} onClick={() => setPage((p) => Math.max(0, p - 1))}>
          ← anterior
        </button>
        <span className="card-subtitle">
          {total === 0 ? '0' : `${page * PAGE_SIZE + 1}-${Math.min(total, (page + 1) * PAGE_SIZE)}`} de {total}
        </span>
        <button type="button" className="btn-link" disabled={(page + 1) * PAGE_SIZE >= total} onClick={() => setPage((p) => p + 1)}>
          próxima →
        </button>
      </div>
    </>
  )
}
