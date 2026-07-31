import { api } from '../api'
import { usePoll } from '../hooks/usePoll'
import { formatRelativeTime } from '../format'

export function SessionsView({ tick, onUnauthorized }: { tick: number; onUnauthorized: () => void }) {
  const { data: sessions, error, loading } = usePoll(() => api.sessions(), 20000, onUnauthorized, [tick])
  const { data: statuses } = usePoll(() => api.executorStatuses(), 20000, onUnauthorized, [tick])
  const liveExecutors = statuses?.filter((status) => status.live).length ?? 0

  return (
    <>
      <div className="view-title">Sessões</div>
      {error && <div className="error-banner">{error}</div>}
      {statuses && (
        <div className="stat-grid">
          <div className="stat-tile">
            <div className="label">Sessões</div>
            <div className="value">{sessions?.length ?? '-'}</div>
          </div>
          <div className="stat-tile">
            <div className="label">CLIs ativos</div>
            <div className="value">{liveExecutors}</div>
          </div>
        </div>
      )}
      {!loading && sessions?.length === 0 && (
        <div className="empty-state">
          <div className="prompt">$</div>
          Nenhuma sessão ainda. Crie uma no SessionHub.
        </div>
      )}
      <div className="card-list">
        {sessions?.map((session) => (
          <div className="card" key={session.id}>
            <div className="card-row">
              <div className="card-title">{session.name}</div>
              <span className="status-badge">
                <span
                  className="dot"
                  style={{ background: session.active_instance ? 'var(--state-running)' : 'var(--state-idle)' }}
                />
                {session.active_instance ? 'ativa' : 'ociosa'}
              </span>
            </div>
            <div className="card-subtitle">{session.workspace}</div>
            <div className="card-meta">
              <span>
                atualizada <b>{formatRelativeTime(session.updated_at)}</b>
              </span>
            </div>
          </div>
        ))}
      </div>
    </>
  )
}
