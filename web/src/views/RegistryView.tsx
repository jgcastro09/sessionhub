import { useState } from 'react'
import { api } from '../api'
import { usePoll } from '../hooks/usePoll'
import { Link } from '../router'

export function RegistryView({ projectId, tick, onUnauthorized }: { projectId: string; tick: number; onUnauthorized: () => void }) {
  const [query, setQuery] = useState('')
  const [scanning, setScanning] = useState(false)
  const [scanError, setScanError] = useState<string>()
  const [refreshNonce, setRefreshNonce] = useState(0)

  const { data: health, error: healthError } = usePoll(
    () => api.registryHealth(projectId),
    15000,
    onUnauthorized,
    [projectId, tick, refreshNonce],
  )
  const { data: entries, error: listError, loading } = usePoll(
    () => (query.trim() ? api.registrySearch(projectId, query).then((r) => r.map((x) => x.entry)) : api.registryEntries(projectId)),
    15000,
    onUnauthorized,
    [projectId, tick, refreshNonce, query],
  )

  async function scan() {
    setScanning(true)
    setScanError(undefined)
    try {
      await api.registryScan(projectId)
      setRefreshNonce((n) => n + 1)
    } catch (err) {
      setScanError(err instanceof Error ? err.message : String(err))
    } finally {
      setScanning(false)
    }
  }

  const coverageOK = health && health.missing_paths.length === 0
  return (
    <>
      <div className="view-title-row">
        <div className="view-title">Code Registry</div>
        <button type="button" className="btn-primary" onClick={scan} disabled={scanning}>
          {scanning ? 'escaneando…' : 'escanear'}
        </button>
      </div>
      {scanError && <div className="error-banner">{scanError}</div>}
      {healthError && <div className="error-banner">{healthError}</div>}
      {health && (
        <div className="stat-grid">
          <div className="stat-tile">
            <div className="label">Cobertura</div>
            <div className="value">{coverageOK ? 'OK' : `${health.missing_paths.length} faltando`}</div>
          </div>
          <div className="stat-tile">
            <div className="label">Pendentes de revisão</div>
            <div className="value">{health.stale_hashes.length}</div>
          </div>
        </div>
      )}

      <input
        className="text-input"
        placeholder="buscar por arquivo, símbolo, módulo…"
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        style={{ marginBottom: '0.75rem', width: '100%' }}
      />
      {listError && <div className="error-banner">{listError}</div>}
      {!loading && entries?.length === 0 && (
        <div className="empty-state">
          <div className="prompt">$</div>
          Nenhuma entrada encontrada. Rode um scan.
        </div>
      )}
      <div className="card-list">
        {entries?.map((entry) => (
          <Link to={`/projects/${projectId}/registry/${entry.entry_id}`} key={entry.entry_id} className="card-link">
            <div className="card">
              <div className="card-row">
                <div className="card-title">{entry.path}</div>
                <span className="status-badge">
                  <span className="dot" style={{ background: entry.reviewed ? 'var(--state-running)' : 'var(--state-idle)' }} />
                  {entry.reviewed ? 'revisado' : 'não revisado'}
                </span>
              </div>
              <div className="card-subtitle">
                {entry.language} · {entry.category} {entry.module ? `· ${entry.module}` : ''}
              </div>
            </div>
          </Link>
        ))}
      </div>
    </>
  )
}
