import { useState } from 'react'
import { PairingGate } from './components/PairingGate'
import { ProjectsView } from './views/ProjectsView'
import { MetricsView } from './views/MetricsView'
import { LogsView } from './views/LogsView'
import { QueueView } from './views/QueueView'
import { useLiveTick } from './hooks/useLiveTick'

type Tab = 'projects' | 'metrics' | 'logs' | 'queue'

const TABS: { id: Tab; label: string; glyph: string }[] = [
	{ id: 'projects', label: 'Projetos', glyph: '▣' },
  { id: 'metrics', label: 'Métricas', glyph: '▲' },
  { id: 'logs', label: 'Logs', glyph: '≡' },
  { id: 'queue', label: 'Fila', glyph: '▤' },
]

function App() {
  const [tab, setTab] = useState<Tab>('projects')
  const [needsPairing, setNeedsPairing] = useState(false)
  const { tick, connected } = useLiveTick()

  const handleUnauthorized = () => setNeedsPairing(true)

  if (needsPairing) {
    return <PairingGate onPaired={() => window.location.reload()} />
  }

  return (
    <div className="app-shell">
      <nav className="sidebar">
        <header className="app-header">
          <div className="app-title">
			<span className="prompt">$</span> sessionhub
          </div>
          <span className={`connection-dot ${connected ? 'online' : 'offline'}`} title={connected ? 'conectado' : 'sem conexão'} />
        </header>
        <div className="tabbar">
          {TABS.map((entry) => (
            <button
              key={entry.id}
              type="button"
              className={`tab ${tab === entry.id ? 'active' : ''}`}
              onClick={() => setTab(entry.id)}
            >
              <span className="glyph">{entry.glyph}</span>
              {entry.label}
            </button>
          ))}
        </div>
      </nav>
      <main className="content">
        {tab === 'projects' && <ProjectsView tick={tick} onUnauthorized={handleUnauthorized} />}
        {tab === 'metrics' && <MetricsView tick={tick} onUnauthorized={handleUnauthorized} />}
        {tab === 'logs' && <LogsView tick={tick} onUnauthorized={handleUnauthorized} />}
        {tab === 'queue' && <QueueView tick={tick} onUnauthorized={handleUnauthorized} />}
      </main>
    </div>
  )
}

export default App
