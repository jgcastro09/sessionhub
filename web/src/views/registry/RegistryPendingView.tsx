import { useState } from 'react'
import { api } from '../../api'
import { usePoll } from '../../hooks/usePoll'

export function RegistryPendingView({
  projectId,
  tick,
  onUnauthorized,
}: {
  projectId: string
  tick: number
  onUnauthorized: () => void
}) {
  const [refreshNonce, setRefreshNonce] = useState(0)
  const { data, error, loading } = usePoll(() => api.registryPending(projectId), 15000, onUnauthorized, [projectId, tick, refreshNonce])
  const [includingPath, setIncludingPath] = useState<string>()
  const [justification, setJustification] = useState('')
  const [actionError, setActionError] = useState<string>()

  async function includeFile(path: string) {
    if (!justification.trim()) {
      setActionError('uma justificativa é obrigatória para incluir um arquivo explicitamente.')
      return
    }
    setActionError(undefined)
    try {
      const cfg = await api.registryConfig(projectId)
      cfg.eligibility.explicit_include_reasons = { ...cfg.eligibility.explicit_include_reasons, [path]: justification.trim() }
      await api.saveRegistryConfig(projectId, cfg)
      setIncludingPath(undefined)
      setJustification('')
      setRefreshNonce((n) => n + 1)
    } catch (err) {
      setActionError(err instanceof Error ? err.message : String(err))
    }
  }

  return (
    <>
      <div className="view-title">Pendentes de classificação</div>
      <p className="card-subtitle" style={{ marginBottom: '1rem' }}>
        Arquivos com conteúdo de texto cuja extensão não está na whitelist de elegibilidade. Eles não entram no registry
        automaticamente — inclua-os explicitamente (com justificativa) ou adicione uma regra em Configuração.
      </p>
      {error && <div className="error-banner">{error}</div>}
      {actionError && <div className="error-banner">{actionError}</div>}
      {!loading && data?.length === 0 && <div className="empty-state">nenhum arquivo pendente.</div>}
      <div className="card-list">
        {data?.map((f) => (
          <div className="card" key={f.path}>
            <div className="card-row">
              <div className="card-title">{f.path}</div>
              <span className="card-subtitle">{f.size_bytes} bytes</span>
            </div>
            {f.suggested_reason && <div className="card-subtitle">{f.suggested_reason}</div>}
            {includingPath === f.path ? (
              <div className="inline-form-column" style={{ marginTop: '0.5rem' }}>
                <input
                  className="text-input"
                  placeholder="justificativa (obrigatória)"
                  value={justification}
                  onChange={(e) => setJustification(e.target.value)}
                  autoFocus
                />
                <div className="card-actions">
                  <button type="button" className="btn-primary" onClick={() => includeFile(f.path)}>
                    confirmar inclusão
                  </button>
                  <button type="button" className="btn-link" onClick={() => setIncludingPath(undefined)}>
                    cancelar
                  </button>
                </div>
              </div>
            ) : (
              <button type="button" className="btn-link" onClick={() => setIncludingPath(f.path)}>
                incluir explicitamente
              </button>
            )}
          </div>
        ))}
      </div>
    </>
  )
}
