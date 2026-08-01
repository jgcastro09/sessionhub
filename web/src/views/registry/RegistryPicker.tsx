import { api } from '../../api'
import { usePoll } from '../../hooks/usePoll'
import { Link } from '../../router'

// The permanent top-level "Registry" nav item lands here first — Code
// Registry data is always per-project, so this mirrors the TUI's
// activeProject-gated pattern: pick a project, then jump into its actual
// data route (/projects/:id/registry), which stays the stable URL.
export function RegistryPicker({ tick, onUnauthorized }: { tick: number; onUnauthorized: () => void }) {
  const { data: projects, error, loading } = usePoll(() => api.projects(), 20000, onUnauthorized, [tick])

  return (
    <>
      <div className="view-title">Code Registry</div>
      {error && <div className="error-banner">{error}</div>}
      {!loading && projects?.length === 0 && (
        <div className="empty-state">
          <div className="prompt">$</div>
          Nenhum projeto ainda. Crie um no Session Hub.
        </div>
      )}
      {projects && projects.length > 0 && (
        <>
          <p className="card-subtitle" style={{ marginBottom: '1rem' }}>
            Selecione um projeto para abrir o Code Registry.
          </p>
          <div className="card-list">
            {projects.map((project) => (
              <Link to={`/projects/${project.id}/registry`} className="card-link" key={project.id}>
                <div className="card">
                  <div className="card-row">
                    <div className="card-title">{project.name}</div>
                  </div>
                  <div className="card-subtitle">{project.root}</div>
                </div>
              </Link>
            ))}
          </div>
        </>
      )}
    </>
  )
}
