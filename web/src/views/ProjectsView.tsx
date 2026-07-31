import { api } from '../api'
import { usePoll } from '../hooks/usePoll'
import { formatRelativeTime } from '../format'

export function ProjectsView({ tick, onUnauthorized }: { tick: number; onUnauthorized: () => void }) {
  const { data: projects, error, loading } = usePoll(() => api.projects(), 20000, onUnauthorized, [tick])

  return (
    <>
		<div className="view-title">Projetos</div>
      {error && <div className="error-banner">{error}</div>}
		{projects && (
        <div className="stat-grid">
          <div className="stat-tile">
				<div className="label">Projetos</div>
            <div className="value">{projects?.length ?? '-'}</div>
          </div>
          <div className="stat-tile">
				<div className="label">Executors ativos</div>
				<div className="value">-</div>
          </div>
        </div>
      )}
      {!loading && projects?.length === 0 && (
        <div className="empty-state">
          <div className="prompt">$</div>
			Nenhum projeto ainda. Crie um no Session Hub.
        </div>
      )}
      <div className="card-list">
        {projects?.map((project) => (
          <div className="card" key={project.id}>
            <div className="card-row">
              <div className="card-title">{project.name}</div>
              <span className="status-badge">
                <span
                  className="dot"
                  style={{ background: project.active_instance ? 'var(--state-running)' : 'var(--state-idle)' }}
                />
                {project.active_instance ? 'ativa' : 'ociosa'}
              </span>
            </div>
			<div className="card-subtitle">{project.root}</div>
            <div className="card-meta">
              <span>
                atualizada <b>{formatRelativeTime(project.updated_at)}</b>
              </span>
            </div>
          </div>
        ))}
      </div>
    </>
  )
}
