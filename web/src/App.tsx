import { useState } from 'react'
import { PairingGate } from './components/PairingGate'
import { ProjectsView } from './views/ProjectsView'
import { MetricsView } from './views/MetricsView'
import { LogsView } from './views/LogsView'
import { QueueView } from './views/QueueView'
import { TasksBoardView } from './views/TasksBoardView'
import { TaskDetailView } from './views/TaskDetailView'
import { RegistryLayout } from './views/registry/RegistryLayout'
import { RegistryPicker } from './views/registry/RegistryPicker'
import { useLiveTick } from './hooks/useLiveTick'
import { RouterProvider, usePath, useNavigate, matchPath, Link } from './router'

type Tab = 'projects' | 'metrics' | 'logs' | 'queue' | 'registry'

const TABS: { id: Tab; label: string; glyph: string }[] = [
	{ id: 'projects', label: 'Projetos', glyph: '▣' },
  { id: 'registry', label: 'Registry', glyph: '◈' },
  { id: 'metrics', label: 'Métricas', glyph: '▲' },
  { id: 'logs', label: 'Logs', glyph: '≡' },
  { id: 'queue', label: 'Fila', glyph: '▤' },
]

// A Project-scoped route (tasks or registry) replaces the dashboard's own
// content pane entirely, but keeps the same sidebar/shell — it is reached by
// a deep link or by following a Link from ProjectsView, per plan 6.3. The
// registry data routes (/projects/:id/registry[/:entryID]) stay exactly as
// they were before the top-level "Registry" nav tab existed — that tab is a
// new entry point into the same routes, not a URL restructuring.
function ProjectScopedContent({ path, tick, onUnauthorized }: { path: string; tick: number; onUnauthorized: () => void }) {
  let params = matchPath('/projects/:projectID/tasks/:taskID', path)
  if (params) return <TaskDetailView projectId={params.projectID} taskId={params.taskID} tick={tick} onUnauthorized={onUnauthorized} />

  params = matchPath('/projects/:projectID/tasks', path)
  if (params) return <TasksBoardView projectId={params.projectID} tick={tick} onUnauthorized={onUnauthorized} />

  params = matchPath('/projects/:projectID/registry/:entryID', path)
  if (params) return <RegistryLayout projectId={params.projectID} entryId={params.entryID} tick={tick} onUnauthorized={onUnauthorized} />

  params = matchPath('/projects/:projectID/registry', path)
  if (params) return <RegistryLayout projectId={params.projectID} tick={tick} onUnauthorized={onUnauthorized} />

  return null
}

function AppShell() {
  const [tab, setTab] = useState<Tab>('projects')
  const [needsPairing, setNeedsPairing] = useState(false)
  const { tick, connected } = useLiveTick()
  const path = usePath()
  const navigate = useNavigate()

  const handleUnauthorized = () => setNeedsPairing(true)

  if (needsPairing) {
    return <PairingGate onPaired={() => window.location.reload()} />
  }

  const projectScoped = path.startsWith('/projects/') && (path.includes('/tasks') || path.includes('/registry'))

  return (
    <div className="app-shell">
      <nav className="sidebar">
        <header className="app-header">
          <div className="app-title">
            <Link to="/" className="app-title-link">
              <span className="prompt">$</span> sessionhub
            </Link>
          </div>
          <span className={`connection-dot ${connected ? 'online' : 'offline'}`} title={connected ? 'conectado' : 'sem conexão'} />
        </header>
        <div className="tabbar">
          {TABS.map((entry) => (
            <button
              key={entry.id}
              type="button"
              className={`tab ${!projectScoped && tab === entry.id ? 'active' : ''}`}
              onClick={() => {
                setTab(entry.id)
                if (path !== '/') navigate('/')
              }}
            >
              <span className="glyph">{entry.glyph}</span>
              {entry.label}
            </button>
          ))}
        </div>
      </nav>
      <main className="content">
        {projectScoped ? (
          <ProjectScopedContent path={path} tick={tick} onUnauthorized={handleUnauthorized} />
        ) : (
          <>
            {tab === 'projects' && <ProjectsView tick={tick} onUnauthorized={handleUnauthorized} />}
            {tab === 'registry' && <RegistryPicker tick={tick} onUnauthorized={handleUnauthorized} />}
            {tab === 'metrics' && <MetricsView tick={tick} onUnauthorized={handleUnauthorized} />}
            {tab === 'logs' && <LogsView tick={tick} onUnauthorized={handleUnauthorized} />}
            {tab === 'queue' && <QueueView tick={tick} onUnauthorized={handleUnauthorized} />}
          </>
        )}
      </main>
    </div>
  )
}

function App() {
  return (
    <RouterProvider>
      <AppShell />
    </RouterProvider>
  )
}

export default App
