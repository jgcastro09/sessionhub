import { useState } from 'react'
import { api } from '../../api'
import { usePoll } from '../../hooks/usePoll'
import type { RegistrySearchQuery } from '../../types'

const PAGE_SIZE = 25

const REVIEW_LABEL: Record<string, string> = { needs_review: 'precisa revisão', reviewed: 'revisado' }

export function RegistryAllFilesView({
  projectId,
  tick,
  onUnauthorized,
  onOpenEntry,
  initialFilters,
}: {
  projectId: string
  tick: number
  onUnauthorized: () => void
  onOpenEntry: (entryId: string) => void
  initialFilters?: Partial<RegistrySearchQuery>
}) {
  const [query, setQuery] = useState('')
  const [category, setCategory] = useState(initialFilters?.category ?? '')
  const [language, setLanguage] = useState('')
  const [reviewStatus, setReviewStatus] = useState('')
  const [criticality, setCriticality] = useState('')
  const [module, setModule] = useState(initialFilters?.module ?? '')
  const [area, setArea] = useState(initialFilters?.area ?? '')
  const [page, setPage] = useState(0)

  const q: RegistrySearchQuery = {
    query: query || undefined,
    category: category || undefined,
    language: language || undefined,
    review_status: reviewStatus || undefined,
    criticality: criticality || undefined,
    module: module || undefined,
    area: area || undefined,
    limit: PAGE_SIZE,
    offset: page * PAGE_SIZE,
  }

  const { data, error, loading } = usePoll(
    () => api.registryEntries(projectId, q),
    15000,
    onUnauthorized,
    [projectId, tick, query, category, language, reviewStatus, criticality, module, area, page],
  )

  const results = data?.results ?? []
  const total = data?.total ?? 0

  return (
    <>
      <div className="view-title">Todos os arquivos</div>
      <div className="registry-filter-bar">
        <input
          className="text-input"
          placeholder="buscar por texto"
          value={query}
          onChange={(e) => {
            setQuery(e.target.value)
            setPage(0)
          }}
        />
        <input
          className="text-input"
          placeholder="categoria"
          value={category}
          onChange={(e) => {
            setCategory(e.target.value)
            setPage(0)
          }}
        />
        <input
          className="text-input"
          placeholder="linguagem"
          value={language}
          onChange={(e) => {
            setLanguage(e.target.value)
            setPage(0)
          }}
        />
        <input
          className="text-input"
          placeholder="módulo"
          value={module}
          onChange={(e) => {
            setModule(e.target.value)
            setPage(0)
          }}
        />
        <input
          className="text-input"
          placeholder="área"
          value={area}
          onChange={(e) => {
            setArea(e.target.value)
            setPage(0)
          }}
        />
        <select
          className="text-input"
          value={reviewStatus}
          onChange={(e) => {
            setReviewStatus(e.target.value)
            setPage(0)
          }}
        >
          <option value="">status de revisão</option>
          <option value="needs_review">precisa revisão</option>
          <option value="reviewed">revisado</option>
        </select>
        <select
          className="text-input"
          value={criticality}
          onChange={(e) => {
            setCriticality(e.target.value)
            setPage(0)
          }}
        >
          <option value="">criticidade</option>
          <option value="standard">padrão</option>
          <option value="important">importante</option>
          <option value="critical">crítica</option>
        </select>
      </div>
      {error && <div className="error-banner">{error}</div>}
      {!loading && results.length === 0 && <div className="empty-state">nenhum arquivo encontrado com esses filtros.</div>}
      {results.length > 0 && (
        <div className="registry-table-wrap">
          <table className="registry-table">
            <thead>
              <tr>
                <th>caminho</th>
                <th>linguagem</th>
                <th>categoria</th>
                <th>módulo</th>
                <th>criticidade</th>
                <th>revisão</th>
              </tr>
            </thead>
            <tbody>
              {results.map((r) => (
                <tr key={r.entry.entry_id} onClick={() => onOpenEntry(r.entry.entry_id)}>
                  <td>{r.entry.path}</td>
                  <td>{r.entry.language}</td>
                  <td>{r.entry.category}</td>
                  <td>{r.entry.module || '—'}</td>
                  <td>{r.entry.criticality || '—'}</td>
                  <td>
                    <span className={`registry-review-badge ${r.entry.review_status}`}>{REVIEW_LABEL[r.entry.review_status]}</span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      <div className="registry-pagination">
        <button type="button" className="btn-link" disabled={page === 0} onClick={() => setPage((p) => Math.max(0, p - 1))}>
          ← anterior
        </button>
        <span className="card-subtitle">
          {total === 0 ? '0' : `${page * PAGE_SIZE + 1}-${Math.min(total, (page + 1) * PAGE_SIZE)}`} de {total}
        </span>
        <button
          type="button"
          className="btn-link"
          disabled={(page + 1) * PAGE_SIZE >= total}
          onClick={() => setPage((p) => p + 1)}
        >
          próxima →
        </button>
      </div>
    </>
  )
}
