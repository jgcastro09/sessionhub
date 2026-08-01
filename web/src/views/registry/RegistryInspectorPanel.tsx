import { useEffect, useState } from 'react'
import { api } from '../../api'
import { usePoll } from '../../hooks/usePoll'
import type { RegistryCriticality, RegistryEntry } from '../../types'

const CRITICALITY_LABEL: Record<string, string> = {
  '': 'não definida',
  standard: 'padrão',
  important: 'importante',
  critical: 'crítica',
}

const REVIEW_LABEL: Record<string, string> = { needs_review: 'precisa revisão', reviewed: 'revisado' }

// The universal detail surface: opened from any file/area/module click in
// any other registry view. Replaces the old standalone RegistryEntryView —
// there is no separate detail page anymore, just this panel over whatever
// section is active.
export function RegistryInspectorPanel({
  projectId,
  entryId,
  tick,
  onUnauthorized,
  onClose,
  onNavigateEntry,
  onOpenReader,
  onOpenRelationships,
}: {
  projectId: string
  entryId: string
  tick: number
  onUnauthorized: () => void
  onClose: () => void
  onNavigateEntry: (entryId: string) => void
  onOpenReader: (entryId: string) => void
  onOpenRelationships: (entryId: string) => void
}) {
  const [refreshNonce, setRefreshNonce] = useState(0)
  const {
    data: entry,
    error,
    loading,
  } = usePoll(() => api.registryEntry(projectId, entryId), 20000, onUnauthorized, [projectId, entryId, tick, refreshNonce])
  const [reviewing, setReviewing] = useState(false)

  useEffect(() => {
    setReviewing(false)
  }, [entryId])

  return (
    <div className="inspector-overlay" onClick={onClose}>
      <div className="inspector-panel" onClick={(e) => e.stopPropagation()}>
        <div className="inspector-header">
          <button type="button" className="btn-link" onClick={onClose}>
            ✕ fechar
          </button>
        </div>
        {error && <div className="error-banner">{error}</div>}
        {loading && !entry && <div className="empty-state">carregando…</div>}
        {entry && (
          <>
            <div className="view-title">{entry.path}</div>
            <div className="card-meta" style={{ marginBottom: '1rem' }}>
              <span>
                linguagem: <b>{entry.language}</b>
              </span>
              <span>
                tipo: <b>{entry.kind}</b>
              </span>
              <span>
                categoria: <b>{entry.category}</b>
              </span>
              <span>
                linhas: <b>{entry.line_count}</b>
              </span>
              <span>
                status: <b>{entry.status}</b>
              </span>
              <span className={`registry-review-badge ${entry.review_status}`}>{REVIEW_LABEL[entry.review_status]}</span>
            </div>
            <div className="card-meta" style={{ marginBottom: '1rem' }}>
              <span>
                módulo: <b>{entry.module || '—'}</b>
              </span>
              <span>
                área: <b>{entry.area || '—'}</b>
              </span>
              <span>
                papel: <b>{entry.role || '—'}</b>
              </span>
              <span>
                criticidade: <b>{CRITICALITY_LABEL[entry.criticality ?? '']}</b>
              </span>
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
                {reviewing ? 'cancelar' : entry.review_status === 'reviewed' ? 'editar revisão' : 'revisar'}
              </button>
              {reviewing && (
                <ReviewForm
                  projectId={projectId}
                  entryId={entryId}
                  initial={entry}
                  onSaved={() => {
                    setReviewing(false)
                    setRefreshNonce((n) => n + 1)
                  }}
                />
              )}
            </div>

            {entry.symbols && Object.keys(entry.symbols).length > 0 && (
              <div className="card" style={{ marginBottom: '1rem' }}>
                <div className="card-title">Símbolos</div>
                {Object.entries(entry.symbols).map(([category, refs]) => (
                  <details key={category} open>
                    <summary>
                      {category} ({refs.length})
                    </summary>
                    <ul>
                      {refs.map((s) => (
                        <li key={`${s.name}:${s.line}`}>
                          {s.name} <span className="card-subtitle">L{s.line}</span>
                        </li>
                      ))}
                    </ul>
                  </details>
                ))}
              </div>
            )}

            {((entry.imports && entry.imports.length > 0) || (entry.includes && entry.includes.length > 0)) && (
              <div className="card" style={{ marginBottom: '1rem' }}>
                <div className="card-title">Dependências</div>
                {entry.includes && entry.includes.length > 0 && (
                  <div className="card-subtitle">includes: {entry.includes.join(', ')}</div>
                )}
                {entry.imports && entry.imports.length > 0 && <div className="card-subtitle">imports: {entry.imports.join(', ')}</div>}
                {entry.exports && entry.exports.length > 0 && <div className="card-subtitle">exports: {entry.exports.join(', ')}</div>}
              </div>
            )}

            {((entry.related_files && entry.related_files.length > 0) ||
              (entry.probable_related_files && entry.probable_related_files.length > 0)) && (
              <div className="card" style={{ marginBottom: '1rem' }}>
                <div className="card-title">Relacionados</div>
                {entry.related_files?.map((id) => (
                  <div className="card-row" key={id}>
                    <button type="button" className="btn-link" onClick={() => onNavigateEntry(id)}>
                      confirmado: {id}
                    </button>
                  </div>
                ))}
                {entry.probable_related_files?.map((id) => (
                  <div className="card-row" key={id}>
                    <button type="button" className="btn-link" onClick={() => onNavigateEntry(id)}>
                      provável: {id}
                    </button>
                  </div>
                ))}
              </div>
            )}

            {entry.previous_paths && entry.previous_paths.length > 0 && (
              <div className="card" style={{ marginBottom: '1rem' }}>
                <div className="card-title">Histórico de renomeação</div>
                <div className="card-subtitle">{entry.previous_paths.join(' → ')}</div>
              </div>
            )}

            <div className="card-actions" style={{ marginBottom: '1rem' }}>
              <button type="button" className="btn-primary" onClick={() => onOpenReader(entryId)}>
                abrir no leitor
              </button>
              <button type="button" className="btn-primary" onClick={() => onOpenRelationships(entryId)}>
                explorar relações
              </button>
            </div>

            <div className="card-meta">
              <span>
                hash: <code>{entry.hash.slice(0, 12)}</code>
              </span>
              <span>
                revisado em: <b>{entry.reviewed_at ? new Date(entry.reviewed_at).toLocaleString() : '—'}</b>
              </span>
              <span>
                último scan: <b>{new Date(entry.last_scanned_at).toLocaleString()}</b>
              </span>
            </div>
          </>
        )}
      </div>
    </div>
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
  initial: RegistryEntry
  onSaved: () => void
}) {
  const [description, setDescription] = useState(initial.description ?? '')
  const [criticality, setCriticality] = useState<RegistryCriticality>(initial.criticality ?? 'standard')
  const [responsibilities, setResponsibilities] = useState((initial.responsibilities ?? []).join(', '))
  const [relatedFiles, setRelatedFiles] = useState((initial.related_files ?? []).join(', '))
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string>()

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setSaving(true)
    setError(undefined)
    try {
      await api.reviewRegistryEntry(projectId, entryId, {
        description,
        criticality,
        responsibilities: responsibilities
          .split(',')
          .map((s) => s.trim())
          .filter(Boolean),
        related_files: relatedFiles
          .split(',')
          .map((s) => s.trim())
          .filter(Boolean),
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
      <input
        className="text-input"
        placeholder="entry_ids relacionados (separados por vírgula)"
        value={relatedFiles}
        onChange={(e) => setRelatedFiles(e.target.value)}
      />
      <select className="text-input" value={criticality} onChange={(e) => setCriticality(e.target.value as RegistryCriticality)}>
        <option value="standard">criticidade padrão</option>
        <option value="important">criticidade importante</option>
        <option value="critical">criticidade crítica</option>
      </select>
      <button type="submit" className="btn-primary" disabled={saving}>
        {saving ? 'salvando…' : 'salvar revisão'}
      </button>
    </form>
  )
}
