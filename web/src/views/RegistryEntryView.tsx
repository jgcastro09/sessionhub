import { useState } from 'react'
import { api } from '../api'
import { usePoll } from '../hooks/usePoll'
import { Link } from '../router'

export function RegistryEntryView({
  projectId,
  entryId,
  tick,
  onUnauthorized,
}: {
  projectId: string
  entryId: string
  tick: number
  onUnauthorized: () => void
}) {
  const { data: entry, error, loading } = usePoll(() => api.registryEntry(projectId, entryId), 15000, onUnauthorized, [
    projectId,
    entryId,
    tick,
  ])
  const { data: context } = usePoll(() => api.registryContext(projectId, entryId), 20000, onUnauthorized, [projectId, entryId, tick])
  const [showSource, setShowSource] = useState(false)
  const { data: source, error: sourceError } = usePoll(
    () => (showSource ? api.registrySource(projectId, entryId) : Promise.resolve(undefined)),
    60000,
    onUnauthorized,
    [projectId, entryId, showSource],
  )

  const [reviewing, setReviewing] = useState(false)

  if (error) return <div className="error-banner">{error}</div>
  if (loading && !entry) return <div className="empty-state">carregando…</div>
  if (!entry) return null

  return (
    <>
      <Link to={`/projects/${projectId}/registry`} className="back-link">
        ← registry
      </Link>
      <div className="view-title">{entry.path}</div>
      <div className="card-meta" style={{ marginBottom: '1rem' }}>
        <span>linguagem: <b>{entry.language}</b></span>
        <span>categoria: <b>{entry.category}</b></span>
        <span>linhas: <b>{entry.lines}</b></span>
        <span>status: <b>{entry.status}</b></span>
      </div>

      <div className="card" style={{ marginBottom: '1rem' }}>
        <div className="card-title">Descrição</div>
        {entry.description ? <p>{entry.description}</p> : <p className="card-subtitle">sem descrição ainda</p>}
        {entry.responsibilities && entry.responsibilities.length > 0 && (
          <>
            <div className="card-title">Responsabilidades</div>
            <ul>
              {entry.responsibilities.map((r) => (
                <li key={r}>{r}</li>
              ))}
            </ul>
          </>
        )}
        <button type="button" className="btn-primary" onClick={() => setReviewing((v) => !v)}>
          {reviewing ? 'cancelar' : entry.reviewed ? 'editar revisão' : 'revisar'}
        </button>
        {reviewing && (
          <ReviewForm
            projectId={projectId}
            entryId={entryId}
            initial={entry}
            onSaved={() => setReviewing(false)}
          />
        )}
      </div>

      {entry.symbols && entry.symbols.length > 0 && (
        <div className="card" style={{ marginBottom: '1rem' }}>
          <div className="card-title">Símbolos</div>
          <div className="card-subtitle">{entry.symbols.join(', ')}</div>
        </div>
      )}

      {context && context.related.length > 0 && (
        <div className="card" style={{ marginBottom: '1rem' }}>
          <div className="card-title">Relacionados</div>
          {context.related.map((r) => (
            <div className="card-row" key={r.entry.entry_id}>
              <Link to={`/projects/${projectId}/registry/${r.entry.entry_id}`}>{r.entry.path}</Link>
            </div>
          ))}
        </div>
      )}

      <div className="view-title-row">
        <button type="button" className="btn-primary" onClick={() => setShowSource((v) => !v)}>
          {showSource ? 'ocultar arquivo' : 'ler arquivo'}
        </button>
      </div>
      {sourceError && <div className="error-banner">{sourceError}</div>}
      {showSource && source && (
        <pre className="section-body reader">{source.content}</pre>
      )}
    </>
  )
}

function ReviewForm({
  projectId,
  entryId,
  initial,
  onSaved,
}: {
  projectId: string
  entryId: string
  initial: { module?: string; description?: string; criticality?: string; responsibilities?: string[] }
  onSaved: () => void
}) {
  const [module, setModule] = useState(initial.module ?? '')
  const [description, setDescription] = useState(initial.description ?? '')
  const [criticality, setCriticality] = useState(initial.criticality ?? 'medium')
  const [responsibilities, setResponsibilities] = useState((initial.responsibilities ?? []).join(', '))
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string>()

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setSaving(true)
    setError(undefined)
    try {
      await api.reviewRegistryEntry(projectId, entryId, {
        module,
        description,
        criticality,
        responsibilities: responsibilities.split(',').map((s) => s.trim()).filter(Boolean),
        relations_confirmed: [],
        relations_probable: [],
      })
      onSaved()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setSaving(false)
    }
  }

  return (
    <form className="inline-form-column" onSubmit={submit}>
      {error && <div className="error-banner">{error}</div>}
      <input className="text-input" placeholder="módulo" value={module} onChange={(e) => setModule(e.target.value)} />
      <textarea
        className="section-body-input"
        placeholder="descrição"
        rows={3}
        value={description}
        onChange={(e) => setDescription(e.target.value)}
      />
      <input
        className="text-input"
        placeholder="responsabilidades (separadas por vírgula)"
        value={responsibilities}
        onChange={(e) => setResponsibilities(e.target.value)}
      />
      <select className="text-input" value={criticality} onChange={(e) => setCriticality(e.target.value)}>
        <option value="low">criticidade baixa</option>
        <option value="medium">criticidade média</option>
        <option value="high">criticidade alta</option>
      </select>
      <button type="submit" className="btn-primary" disabled={saving}>
        {saving ? 'salvando…' : 'salvar revisão'}
      </button>
    </form>
  )
}
