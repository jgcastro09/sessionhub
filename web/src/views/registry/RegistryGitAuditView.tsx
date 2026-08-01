import { api } from '../../api'
import { usePoll } from '../../hooks/usePoll'

export function RegistryGitAuditView({
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
  const { data, error, loading } = usePoll(() => api.registryGitStatus(projectId), 15000, onUnauthorized, [projectId, tick])

  return (
    <>
      <div className="view-title">Auditoria Git</div>
      {error && <div className="error-banner">{error}</div>}
      {loading && !data && <div className="empty-state">carregando…</div>}
      {data && !data.git.repository && <div className="empty-state">este projeto não é um repositório Git.</div>}
      {data && data.git.repository && (
        <>
          <div className="card-meta" style={{ marginBottom: '1rem' }}>
            <span>
              branch: <b>{data.git.branch || '—'}</b>
            </span>
            <span>
              upstream: <b>{data.git.upstream || '—'}</b>
            </span>
            <span>
              ahead/behind: <b>{data.git.ahead}/{data.git.behind}</b>
            </span>
            <span>
              limpo: <b>{data.git.clean ? 'sim' : 'não'}</b>
            </span>
            {data.git.conflicted && <span className="registry-review-badge needs_review">com conflitos</span>}
          </div>

          {data.changed_entries.length > 0 && (
            <div className="card" style={{ marginBottom: '1rem' }}>
              <div className="card-title">Entradas do registry com alterações no working tree</div>
              {data.changed_entries.map((id) => (
                <div className="card-row" key={id}>
                  <button type="button" className="btn-link" onClick={() => onOpenEntry(id)}>
                    {id}
                  </button>
                </div>
              ))}
            </div>
          )}

          {data.git.files.length > 0 && (
            <div className="card">
              <div className="card-title">Arquivos alterados</div>
              {data.git.files.map((f) => (
                <div className="card-row" key={f.path}>
                  <span>{f.status}</span> <span>{f.path}</span>
                  {f.conflict && <span className="registry-review-badge needs_review">conflito</span>}
                </div>
              ))}
            </div>
          )}
        </>
      )}
    </>
  )
}
