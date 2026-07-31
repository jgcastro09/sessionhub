import { api } from '../api'
import { usePoll } from '../hooks/usePoll'

const LEVEL_COLOR: Record<string, string> = {
  error: 'var(--state-failed)',
  warn: 'var(--state-queued)',
  info: 'var(--state-running)',
  debug: 'var(--text-dim)',
}

export function LogsView({ tick, onUnauthorized }: { tick: number; onUnauthorized: () => void }) {
	const { data: projects } = usePoll(() => api.projects(), 20000, onUnauthorized, [tick])
	const { data: logs, error, loading } = usePoll(
		async () => (await Promise.all((projects ?? []).map((project) => api.logs(project.id, 100)))).flat(),
		20000, onUnauthorized, [tick, projects?.map((project) => project.id).join(',')],
	)

  return (
    <>
      <div className="view-title">Logs</div>
      {error && <div className="error-banner">{error}</div>}
      {!loading && logs?.length === 0 && (
        <div className="empty-state">
          <div className="prompt">$</div>
          Nenhum log recente.
        </div>
      )}
      <div className="card">
        {logs?.map((entry) => (
          <div className="log-line" key={entry.id}>
            <span className="log-time">{new Date(entry.created_at).toLocaleTimeString()}</span>
            <span className="log-level" style={{ color: LEVEL_COLOR[entry.level] ?? 'var(--text-dim)' }}>
              {entry.level}
            </span>
            <span className="log-message">
              <b>{entry.kind}</b> {entry.message}
            </span>
          </div>
        ))}
      </div>
    </>
  )
}
