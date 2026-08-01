import { useState } from 'react'
import { api } from '../../api'
import { usePoll } from '../../hooks/usePoll'

export function RegistryGlobalSearchView({
  projectId,
  tick,
  onUnauthorized,
  onOpenEntry,
}: {
  projectId: string
  tick: number
  onUnauthorized: () => void
  onOpenEntry: (entryId: string) => void
}) {
  const [text, setText] = useState('')
  const [category, setCategory] = useState('')
  const [language, setLanguage] = useState('')
  const [semantic, setSemantic] = useState(false)

  const { data, error, loading } = usePoll(
    () =>
      text
        ? api.registrySearch(projectId, {
            query: text,
            category: category || undefined,
            language: language || undefined,
            semantic,
            limit: 50,
          })
        : Promise.resolve(undefined),
    15000,
    onUnauthorized,
    [projectId, tick, text, category, language, semantic],
  )

  return (
    <>
      <div className="view-title">Busca global</div>
      <div className="registry-filter-bar">
        <input
          className="text-input"
          placeholder="texto, símbolo, caminho…"
          value={text}
          onChange={(e) => setText(e.target.value)}
          autoFocus
        />
        <input className="text-input" placeholder="categoria" value={category} onChange={(e) => setCategory(e.target.value)} />
        <input className="text-input" placeholder="linguagem" value={language} onChange={(e) => setLanguage(e.target.value)} />
        <label className="card-subtitle">
          <input type="checkbox" checked={semantic} onChange={(e) => setSemantic(e.target.checked)} /> busca semântica
        </label>
      </div>
      {error && <div className="error-banner">{error}</div>}
      {loading && text && <div className="empty-state">buscando…</div>}
      {!loading && text && data?.results.length === 0 && <div className="empty-state">nenhum resultado.</div>}
      {data && data.results.length > 0 && (
        <div className="card-list">
          {data.results.map((r) => (
            <div className="card" key={r.entry.entry_id} onClick={() => onOpenEntry(r.entry.entry_id)} style={{ cursor: 'pointer' }}>
              <div className="card-row">
                <div className="card-title">{r.entry.path}</div>
                <span className="card-subtitle">score {r.score}</span>
              </div>
              <div className="card-subtitle">
                {r.entry.language} · {r.entry.category}
                {r.match_reasons && r.match_reasons.length > 0 ? ` · ${r.match_reasons.join(', ')}` : ''}
              </div>
            </div>
          ))}
        </div>
      )}
    </>
  )
}
