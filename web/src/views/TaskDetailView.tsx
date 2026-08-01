import { useEffect, useState } from 'react'
import { api, NotFoundError } from '../api'
import { usePoll } from '../hooks/usePoll'
import { Link } from '../router'
import type { AuditReport, RegistryEntry, Task, TaskStatus } from '../types'

const STATUS_LABEL: Record<TaskStatus, string> = {
  idea: 'Ideia',
  backlog: 'Backlog',
  ready: 'Pronta',
  in_progress: 'Em andamento',
  changes_requested: 'Ajustes solicitados',
  done: 'Concluída',
  archived: 'Arquivada',
}

const ALL_STATUSES: TaskStatus[] = ['idea', 'backlog', 'ready', 'in_progress', 'changes_requested', 'done', 'archived']

export function TaskDetailView({
  projectId,
  taskId,
  tick,
  onUnauthorized,
}: {
  projectId: string
  taskId: string
  tick: number
  onUnauthorized: () => void
}) {
  const { data: task, error, loading } = usePoll(() => api.task(projectId, taskId), 15000, onUnauthorized, [projectId, taskId, tick])
  const [statusError, setStatusError] = useState<string>()
  const [changingStatus, setChangingStatus] = useState(false)
  const [audit, setAudit] = useState<AuditReport>()
  const [auditing, setAuditing] = useState(false)
  const [auditError, setAuditError] = useState<string>()
  const [reloadNonce, setReloadNonce] = useState(0)
  const [liveTask, setLiveTask] = useState<Task | undefined>(undefined)
  const [editedSections, setEditedSections] = useState<Record<string, string>>({})
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<string>()

  useEffect(() => setLiveTask(task), [task])
  const current = liveTask ?? task

  async function saveSections() {
    if (!current || Object.keys(editedSections).length === 0) return
    setSaving(true)
    setSaveError(undefined)
    try {
      const updated = await api.patchTask(projectId, taskId, { sections: editedSections })
      setLiveTask(updated)
      setEditedSections({})
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : String(err))
    } finally {
      setSaving(false)
    }
  }

  async function changeStatus(status: TaskStatus) {
    if (!current || status === current.status) return
    setChangingStatus(true)
    setStatusError(undefined)
    try {
      const updated = await api.patchTask(projectId, taskId, { status })
      setLiveTask(updated)
    } catch (err) {
      setStatusError(err instanceof Error ? err.message : String(err))
    } finally {
      setChangingStatus(false)
    }
  }

  async function runAudit() {
    setAuditing(true)
    setAuditError(undefined)
    try {
      const report = await api.auditTask(projectId, taskId)
      setAudit(report)
      setReloadNonce((n) => n + 1)
    } catch (err) {
      setAuditError(err instanceof Error ? err.message : String(err))
    } finally {
      setAuditing(false)
    }
  }

  if (error) return <div className="error-banner">{error}</div>
  if (loading && !current) return <div className="empty-state">carregando…</div>
  if (!current) return null

  return (
    <>
      <div className="view-title-row">
        <div>
          <Link to={`/projects/${projectId}/tasks`} className="back-link">
            ← tarefas
          </Link>
          <div className="view-title">
            {current.id} — {current.title}
          </div>
        </div>
        <select
          className="text-input"
          value={current.status}
          disabled={changingStatus}
          onChange={(e) => changeStatus(e.target.value as TaskStatus)}
        >
          {ALL_STATUSES.map((s) => (
            <option key={s} value={s}>
              {STATUS_LABEL[s]}
            </option>
          ))}
        </select>
      </div>
      {statusError && <div className="error-banner">{statusError}</div>}

      <div className="card-meta" style={{ marginBottom: '1rem' }}>
        <span>tipo: <b>{current.type}</b></span>
        <span>prioridade: <b>{current.priority}</b></span>
        <span>atualizada: <b>{new Date(current.updated_at).toLocaleString('pt-BR')}</b></span>
      </div>

      <TaskMetadataPanel
        task={current}
        onSave={async (patch) => {
          const updated = await api.patchTask(projectId, taskId, patch)
          setLiveTask(updated)
        }}
      />

      <TechnicalContextPanel
        projectId={projectId}
        registryRefs={current.registry_refs}
        onAttach={async (entryId) => {
          const ref = `entry:${entryId}`
          if (current.registry_refs.includes(ref)) return
          const updated = await api.patchTask(projectId, taskId, { registry_refs: [...current.registry_refs, ref] })
          setLiveTask(updated)
        }}
      />

      <ClaimsPanel projectId={projectId} taskId={taskId} tick={tick} onUnauthorized={onUnauthorized} />

      <div className="section-editor">
        {current.sections.map((section) => {
          const readOnly = section.heading === 'Audit Report'
          const value = editedSections[section.heading] ?? section.body
          return (
            <div className="card" key={section.heading} style={{ marginBottom: '0.75rem' }}>
              <div className="card-title">{section.heading}</div>
              <textarea
                className="section-body-input"
                readOnly={readOnly}
                value={value}
                rows={section.heading === 'Notas e histórico' ? 4 : 3}
                onChange={(e) => setEditedSections((prev) => ({ ...prev, [section.heading]: e.target.value }))}
              />
            </div>
          )
        })}
      </div>
      {saveError && <div className="error-banner">{saveError}</div>}
      <div className="view-title-row">
        <button type="button" className="btn-primary" onClick={saveSections} disabled={saving || Object.keys(editedSections).length === 0}>
          {saving ? 'salvando…' : 'salvar alterações'}
        </button>
        <button type="button" className="btn-primary" onClick={runAudit} disabled={auditing}>
          {auditing ? 'auditando…' : 'rodar auditoria'}
        </button>
      </div>
      {auditError && <div className="error-banner">{auditError}</div>}
      {audit && (
        <div className="card" key={reloadNonce}>
          <div className="card-title">
            resultado: {audit.reproducible_pass ? 'evidência reproduzível completa' : 'evidência incompleta ou pendente'}
          </div>
          {audit.results.map((r, i) => (
            <div className="audit-check" key={i}>
              <span className={`audit-status ${r.resolved ? (r.passed ? 'pass' : 'fail') : 'pending'}`}>
                {!r.resolved ? 'PENDENTE' : r.passed ? 'PASSOU' : 'FALHOU'}
              </span>
              <span>
                {r.check.kind}: {r.check.raw} — {r.detail}
              </span>
            </div>
          ))}
        </div>
      )}
    </>
  )
}

function TaskMetadataPanel({
  task,
  onSave,
}: {
  task: Task
  onSave: (patch: { type: string; priority: string; impacted_areas: string[]; dependencies: string[] }) => Promise<void>
}) {
  const [type, setType] = useState(task.type)
  const [priority, setPriority] = useState(task.priority)
  const [areas, setAreas] = useState(task.impacted_areas.join(', '))
  const [dependencies, setDependencies] = useState(task.dependencies.join(', '))
  const [editing, setEditing] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string>()

  useEffect(() => {
    setType(task.type)
    setPriority(task.priority)
    setAreas(task.impacted_areas.join(', '))
    setDependencies(task.dependencies.join(', '))
  }, [task])

  const split = (value: string) => value.split(',').map((item) => item.trim()).filter(Boolean)

  async function save() {
    setSaving(true)
    setError(undefined)
    try {
      await onSave({ type, priority, impacted_areas: split(areas), dependencies: split(dependencies) })
      setEditing(false)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="card" style={{ marginBottom: '1rem' }}>
      <div className="card-row">
        <div className="card-title">Planejamento</div>
        <button type="button" className="btn-link" onClick={() => setEditing((value) => !value)}>{editing ? 'cancelar' : 'editar metadados'}</button>
      </div>
      {!editing ? (
        <div className="card-meta">
          <span>áreas: <b>{task.impacted_areas.length ? task.impacted_areas.join(', ') : 'não informadas'}</b></span>
          <span>dependências: <b>{task.dependencies.length ? task.dependencies.join(', ') : 'nenhuma'}</b></span>
        </div>
      ) : (
        <div className="inline-form-column" style={{ marginTop: '0.75rem' }}>
          <div className="inline-form" style={{ width: '100%' }}>
            <input className="text-input" value={type} onChange={(event) => setType(event.target.value)} placeholder="tipo" />
            <select className="text-input" value={priority} onChange={(event) => setPriority(event.target.value as Task['priority'])}>
              <option value="low">baixa</option><option value="medium">média</option><option value="high">alta</option><option value="urgent">urgente</option>
            </select>
          </div>
          <input className="text-input" value={areas} onChange={(event) => setAreas(event.target.value)} placeholder="áreas impactadas, separadas por vírgula" />
          <input className="text-input" value={dependencies} onChange={(event) => setDependencies(event.target.value)} placeholder="dependências, separadas por vírgula" />
          {error && <div className="error-banner">{error}</div>}
          <button type="button" className="btn-primary" onClick={save} disabled={saving || !type.trim()}>{saving ? 'salvando…' : 'salvar planejamento'}</button>
        </div>
      )}
    </div>
  )
}

function TechnicalContextPanel({
  projectId,
  registryRefs,
  onAttach,
}: {
  projectId: string
  registryRefs: string[]
  onAttach: (entryId: string) => Promise<void>
}) {
  const entryRefs = registryRefs.filter((r) => r.startsWith('entry:')).map((r) => r.slice('entry:'.length))
  const [entries, setEntries] = useState<Record<string, RegistryEntry | 'broken'>>({})
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<RegistryEntry[]>([])
  const [attaching, setAttaching] = useState<string>()

  useEffect(() => {
    let cancelled = false
    entryRefs.forEach((id) => {
      api
        .registryEntry(projectId, id)
        .then((entry) => {
          if (!cancelled) setEntries((prev) => ({ ...prev, [id]: entry }))
        })
        .catch((err) => {
          if (!cancelled && err instanceof NotFoundError) setEntries((prev) => ({ ...prev, [id]: 'broken' }))
        })
    })
    return () => {
      cancelled = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [projectId, registryRefs.join(',')])

  useEffect(() => {
    if (!query.trim()) {
      setResults([])
      return
    }
    let cancelled = false
    const id = window.setTimeout(() => {
      api
        .registrySearch(projectId, { query, limit: 20 })
        .then((found) => {
          if (!cancelled) setResults(found.results.map((r) => r.entry))
        })
        .catch(() => {
          if (!cancelled) setResults([])
        })
    }, 300)
    return () => {
      cancelled = true
      window.clearTimeout(id)
    }
  }, [projectId, query])

  async function attach(entryId: string) {
    setAttaching(entryId)
    try {
      await onAttach(entryId)
      setQuery('')
      setResults([])
    } finally {
      setAttaching(undefined)
    }
  }

  return (
    <div className="card" style={{ marginBottom: '1rem' }}>
      <div className="card-title">Contexto técnico</div>
      {entryRefs.length === 0 && <p className="card-subtitle">nenhuma referência do Registry ainda.</p>}
      {entryRefs.map((id) => {
        const entry = entries[id]
        if (!entry) return <div key={id}>carregando {id}…</div>
        if (entry === 'broken') {
          return (
            <div key={id} className="error-banner">
              referência quebrada: entry:{id}
            </div>
          )
        }
        return (
          <div key={id} className="card-row">
            <Link to={`/projects/${projectId}/registry/${entry.entry_id}`}>{entry.path}</Link>
            <span className="card-subtitle">{entry.module}</span>
          </div>
        )
      })}
      <input
        className="text-input"
        placeholder="buscar no Registry para anexar…"
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        style={{ width: '100%', marginTop: '0.5rem' }}
      />
      {results.map((entry) => (
        <div className="card-row" key={entry.entry_id}>
          <span>{entry.path}</span>
          <button
            type="button"
            className="btn-link"
            disabled={attaching === entry.entry_id}
            onClick={() => attach(entry.entry_id)}
          >
            anexar
          </button>
        </div>
      ))}
    </div>
  )
}

function ClaimsPanel({
  projectId,
  taskId,
  tick,
  onUnauthorized,
}: {
  projectId: string
  taskId: string
  tick: number
  onUnauthorized: () => void
}) {
  const { data: claims } = usePoll(() => api.taskClaims(projectId), 10000, onUnauthorized, [projectId, tick])
  const active = claims?.filter((c) => c.task_id === taskId)
  if (!active || active.length === 0) return null
  return (
    <div className="card" style={{ marginBottom: '1rem' }}>
      <div className="card-title">Em execução</div>
      {active.map((c) => (
        <div className="card-row" key={c.terminal_id}>
          <span>executor {c.executor_id}</span>
          <span className="card-subtitle">desde {new Date(c.claimed_at).toLocaleString('pt-BR')}</span>
        </div>
      ))}
    </div>
  )
}
