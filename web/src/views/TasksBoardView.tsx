import { useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import { usePoll } from '../hooks/usePoll'
import { Link } from '../router'
import type { Task, TaskClaim, TaskImportResult, TaskPriority, TaskStatus } from '../types'

const COLUMNS: { id: TaskStatus; label: string; operational?: boolean }[] = [
  { id: 'idea', label: 'Ideias' },
  { id: 'backlog', label: 'Backlog' },
  { id: 'ready', label: 'Prontas', operational: true },
  { id: 'in_progress', label: 'Em andamento', operational: true },
  { id: 'changes_requested', label: 'Ajustes solicitados', operational: true },
  { id: 'done', label: 'Concluídas' },
  { id: 'archived', label: 'Arquivadas' },
]

const PRIORITY_LABEL: Record<string, string> = { low: 'Baixa', medium: 'Média', high: 'Alta', urgent: 'Urgente' }
const TYPE_OPTIONS = ['feature', 'bug', 'chore', 'spike']
const EMPTY_TASKS: Task[] = []

type Sort = 'updated' | 'priority' | 'created' | 'id'

function splitItems(value: string): string[] {
  return value.split(',').map((item) => item.trim()).filter(Boolean)
}

function taskSummary(task: Task): string {
  return task.sections.find((section) => section.heading === 'Resumo')?.body.trim() ?? ''
}

function auditState(task: Task): 'pass' | 'pending' | 'none' {
  const report = task.sections.find((section) => section.heading === 'Audit Report')?.body ?? ''
  if (report.includes('evidência reproduzível completa')) return 'pass'
  if (report.trim()) return 'pending'
  return 'none'
}

function compareTasks(sort: Sort) {
  const priority: Record<TaskPriority, number> = { urgent: 4, high: 3, medium: 2, low: 1 }
  return (a: Task, b: Task) => {
    if (sort === 'priority') return priority[b.priority] - priority[a.priority] || b.updated_at.localeCompare(a.updated_at)
    if (sort === 'created') return b.created_at.localeCompare(a.created_at)
    if (sort === 'id') return a.id.localeCompare(b.id)
    return b.updated_at.localeCompare(a.updated_at)
  }
}

export function TasksBoardView({ projectId, tick, onUnauthorized }: { projectId: string; tick: number; onUnauthorized: () => void }) {
  const { data: tasks, error, loading } = usePoll(() => api.tasks(projectId), 10000, onUnauthorized, [projectId, tick])
  const { data: claims } = usePoll(() => api.taskClaims(projectId), 10000, onUnauthorized, [projectId, tick])
  const [actionError, setActionError] = useState<string>()
  const [creating, setCreating] = useState(false)
  const [importing, setImporting] = useState(false)
  const [dragTaskId, setDragTaskId] = useState<string>()
  const [localTasks, setLocalTasks] = useState<Task[]>()
  const [query, setQuery] = useState('')
  const [type, setType] = useState('')
  const [priority, setPriority] = useState('')
  const [area, setArea] = useState('')
  const [sort, setSort] = useState<Sort>('updated')
  const [showClosed, setShowClosed] = useState(false)
  const [collapsed, setCollapsed] = useState<Set<TaskStatus>>(() => new Set(['done', 'archived']))

  useEffect(() => setLocalTasks(undefined), [tasks])
  const board = useMemo(() => localTasks ?? tasks ?? EMPTY_TASKS, [localTasks, tasks])
  const areas = useMemo(() => Array.from(new Set(board.flatMap((task) => task.impacted_areas))).sort((a, b) => a.localeCompare(b)), [board])
  const visibleColumns = COLUMNS.filter((column) => showClosed || (column.id !== 'done' && column.id !== 'archived'))
  const filtered = useMemo(() => {
    const normalizedQuery = query.trim().toLocaleLowerCase()
    return board
      .filter((task) => !type || task.type === type)
      .filter((task) => !priority || task.priority === priority)
      .filter((task) => !area || task.impacted_areas.some((item) => item === area))
      .filter((task) => !normalizedQuery || [task.id, task.title, taskSummary(task), ...task.impacted_areas].join(' ').toLocaleLowerCase().includes(normalizedQuery))
      .sort(compareTasks(sort))
  }, [area, board, priority, query, sort, type])

  async function handleDrop(status: TaskStatus) {
    if (!dragTaskId) return
    const taskId = dragTaskId
    setDragTaskId(undefined)
    const task = board.find((candidate) => candidate.id === taskId)
    if (!task || task.status === status) return
    setActionError(undefined)
    setLocalTasks(board.map((candidate) => (candidate.id === taskId ? { ...candidate, status } : candidate)))
    try {
      await api.patchTask(projectId, taskId, { status })
    } catch (err) {
      setActionError(err instanceof Error ? err.message : String(err))
      setLocalTasks(undefined)
    }
  }

  function toggleColumn(status: TaskStatus) {
    setCollapsed((current) => {
      const next = new Set(current)
      if (next.has(status)) next.delete(status)
      else next.add(status)
      return next
    })
  }

  return (
    <>
      <div className="view-title-row task-board-title-row">
        <div>
          <div className="view-title">Tarefas</div>
          <div className="card-subtitle">Planeje, execute, valide e mantenha o contexto técnico no mesmo Project.</div>
        </div>
        <div className="task-board-actions">
          <button type="button" className="btn-secondary" onClick={() => setShowClosed((value) => !value)}>
            {showClosed ? 'ocultar concluídas' : 'ver concluídas'}
          </button>
          <button type="button" className="btn-secondary" onClick={() => { setImporting((value) => !value); setCreating(false) }}>
            {importing ? 'cancelar' : '⇩ importar card'}
          </button>
          <button type="button" className="btn-primary" onClick={() => { setCreating((value) => !value); setImporting(false) }}>
            {creating ? 'cancelar' : '+ nova tarefa'}
          </button>
        </div>
      </div>
      {error && <div className="error-banner">{error}</div>}
      {actionError && <div className="error-banner">{actionError}</div>}
      {importing && <ImportTaskForm projectId={projectId} onImported={() => setImporting(false)} />}
      {creating && <CreateTaskForm projectId={projectId} onCreated={() => setCreating(false)} />}

      <div className="task-filter-bar" aria-label="Filtros de tarefas">
        <input className="text-input task-search-input" placeholder="buscar ID, título, resumo ou área…" value={query} onChange={(event) => setQuery(event.target.value)} />
        <select className="text-input" value={type} onChange={(event) => setType(event.target.value)}>
          <option value="">todos os tipos</option>
          {Array.from(new Set(board.map((task) => task.type))).sort().map((value) => <option key={value} value={value}>{value}</option>)}
        </select>
        <select className="text-input" value={priority} onChange={(event) => setPriority(event.target.value)}>
          <option value="">todas as prioridades</option>
          {Object.entries(PRIORITY_LABEL).map(([value, label]) => <option key={value} value={value}>{label}</option>)}
        </select>
        <select className="text-input" value={area} onChange={(event) => setArea(event.target.value)}>
          <option value="">todas as áreas</option>
          {areas.map((value) => <option key={value} value={value}>{value}</option>)}
        </select>
        <select className="text-input" value={sort} onChange={(event) => setSort(event.target.value as Sort)}>
          <option value="updated">atualizadas primeiro</option>
          <option value="priority">prioridade primeiro</option>
          <option value="created">criadas primeiro</option>
          <option value="id">ID</option>
        </select>
      </div>

      {!loading && board.length === 0 && <div className="empty-state"><div className="prompt">$</div>Nenhuma tarefa ainda. Crie uma para guardar contexto, aceite e evidências junto do Project.</div>}
      {!loading && board.length > 0 && filtered.length === 0 && <div className="empty-state">Nenhuma tarefa corresponde aos filtros atuais.</div>}

      <div className="board task-board">
        {visibleColumns.map((column) => {
          const columnTasks = filtered.filter((task) => task.status === column.id)
          const isCollapsed = collapsed.has(column.id)
          return (
            <div className={`board-column ${isCollapsed ? 'board-column-collapsed' : ''} ${column.operational ? 'board-column-operational' : ''}`} key={column.id} onDragOver={(event) => event.preventDefault()} onDrop={() => handleDrop(column.id)}>
              <button type="button" className="board-column-header" onClick={() => toggleColumn(column.id)} aria-expanded={!isCollapsed}>
                <span>{column.label}</span><span className="board-count">{columnTasks.length}</span>
              </button>
              {!isCollapsed && <div className="board-column-body">
                {columnTasks.map((task) => <TaskCard key={task.id} projectId={projectId} task={task} claims={claims ?? []} onDragStart={() => setDragTaskId(task.id)} />)}
              </div>}
            </div>
          )
        })}
      </div>
    </>
  )
}

function TaskCard({ projectId, task, claims, onDragStart }: { projectId: string; task: Task; claims: TaskClaim[]; onDragStart: () => void }) {
  const activeClaims = claims.filter((claim) => claim.task_id === task.id)
  const audit = auditState(task)
  const summary = taskSummary(task)
  return (
    <div className="board-card task-rich-card" draggable onDragStart={onDragStart}>
      <Link to={`/projects/${projectId}/tasks/${task.id}`} className="board-card-link">
        <div className="task-card-topline"><span className="board-card-id">{task.id}</span><span className={`priority priority-${task.priority}`}>{PRIORITY_LABEL[task.priority]}</span></div>
        <div className="board-card-title">{task.title}</div>
        {summary && <div className="task-card-summary">{summary}</div>}
        <div className="task-card-tags"><span className="task-type-pill">{task.type}</span>{task.impacted_areas.slice(0, 2).map((item) => <span className="task-area-pill" key={item}>{item}</span>)}</div>
        <div className="task-card-signals">
          {activeClaims.length > 0 && <span title="Em execução por um executor">● {activeClaims.length} em execução</span>}
          {task.registry_refs.length > 0 && <span title="Referências técnicas anexadas">◈ {task.registry_refs.length}</span>}
          {task.dependencies.length > 0 && <span title="Dependências">↗ {task.dependencies.length}</span>}
          {audit !== 'none' && <span className={`task-audit-${audit}`}>{audit === 'pass' ? '✓ validada' : '◌ auditoria pendente'}</span>}
        </div>
      </Link>
    </div>
  )
}

function CreateTaskForm({ projectId, onCreated }: { projectId: string; onCreated: () => void }) {
  const [title, setTitle] = useState('')
  const [type, setType] = useState('feature')
  const [priority, setPriority] = useState('medium')
  const [summary, setSummary] = useState('')
  const [areas, setAreas] = useState('')
  const [dependencies, setDependencies] = useState('')
  const [registryRefs, setRegistryRefs] = useState('')
  const [acceptance, setAcceptance] = useState('')
  const [prompt, setPrompt] = useState('')
  const [advanced, setAdvanced] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string>()

  async function submit(event: React.FormEvent) {
    event.preventDefault()
    if (!title.trim()) return
    setSubmitting(true)
    setError(undefined)
    try {
      const task = await api.createTask(projectId, { title, type, priority, impacted_areas: splitItems(areas), dependencies: splitItems(dependencies), registry_refs: splitItems(registryRefs) })
      const sections: Record<string, string> = {}
      if (summary.trim()) sections.Resumo = summary.trim()
      if (acceptance.trim()) sections['Critérios de aceite'] = acceptance.trim()
      if (prompt.trim()) sections['Prompt sugerido'] = prompt.trim()
      if (Object.keys(sections).length > 0) await api.patchTask(projectId, task.id, { sections })
      onCreated()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setSubmitting(false)
    }
  }

  return <form className="task-create-form" onSubmit={submit}>
    <div className="task-create-heading"><b>Nova tarefa</b><span>O card é Markdown portável; o contexto pode viajar com qualquer Project.</span></div>
    {error && <div className="error-banner">{error}</div>}
    <div className="task-create-grid">
      <input className="text-input task-create-title" placeholder="título claro e acionável" value={title} onChange={(event) => setTitle(event.target.value)} autoFocus />
      <select className="text-input" value={type} onChange={(event) => setType(event.target.value)}>{TYPE_OPTIONS.map((value) => <option value={value} key={value}>{value}</option>)}</select>
      <select className="text-input" value={priority} onChange={(event) => setPriority(event.target.value)}>{Object.entries(PRIORITY_LABEL).map(([value, label]) => <option value={value} key={value}>{label}</option>)}</select>
    </div>
    <textarea className="section-body-input" rows={2} placeholder="resumo: por que esta tarefa importa?" value={summary} onChange={(event) => setSummary(event.target.value)} />
    {advanced && <div className="task-create-advanced">
      <input className="text-input" placeholder="áreas impactadas, separadas por vírgula" value={areas} onChange={(event) => setAreas(event.target.value)} />
      <input className="text-input" placeholder="dependências (IDs ou referências), separadas por vírgula" value={dependencies} onChange={(event) => setDependencies(event.target.value)} />
      <input className="text-input" placeholder="referências do Registry (ex.: entry:abc), separadas por vírgula" value={registryRefs} onChange={(event) => setRegistryRefs(event.target.value)} />
      <textarea className="section-body-input" rows={2} placeholder="critérios de aceite" value={acceptance} onChange={(event) => setAcceptance(event.target.value)} />
      <textarea className="section-body-input" rows={2} placeholder="prompt ou contexto sugerido para executor/IA" value={prompt} onChange={(event) => setPrompt(event.target.value)} />
    </div>}
    <div className="task-create-actions"><button type="button" className="btn-link" onClick={() => setAdvanced((value) => !value)}>{advanced ? 'menos campos' : '+ contexto técnico e aceite'}</button><button type="submit" className="btn-primary" disabled={submitting}>{submitting ? 'criando…' : 'criar tarefa'}</button></div>
  </form>
}

function ImportTaskForm({ projectId, onImported }: { projectId: string; onImported: () => void }) {
  const [text, setText] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string>()
  const [result, setResult] = useState<TaskImportResult>()

  async function submit(event: React.FormEvent) {
    event.preventDefault()
    if (!text.trim()) return
    setSubmitting(true)
    setError(undefined)
    setResult(undefined)
    try {
      const response = await api.importTask(projectId, text)
      setResult(response.result)
      if (response.result.valid && response.card) onImported()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setSubmitting(false)
    }
  }

  return <form className="task-create-form" onSubmit={submit}>
    <div className="task-create-heading">
      <b>Importar card</b>
      <span>Cole aqui a saída completa do prompt gerador de Cards (máx. ~9.000 tokens). O card é criado com título, tipo, prioridade, áreas e todas as seções já preenchidas.</span>
    </div>
    {error && <div className="error-banner">{error}</div>}
    {result && !result.valid && <div className="error-banner">{result.errors.join(' ')}</div>}
    <textarea
      className="section-body-input"
      rows={10}
      placeholder="* Título Direto: ...&#10;* Resumo de Uma Frase: ...&#10;* Tipo: ...&#10;* Prioridade: ...&#10;* Áreas Envolvidas: ...&#10;* Descrição Detalhada: ...&#10;* Prompt Detalhado para a IA: ...&#10;* Funcionalidades Esperadas: ...&#10;* Contrato de Auditoria: ..."
      value={text}
      onChange={(event) => setText(event.target.value)}
      autoFocus
    />
    {result && <div className="card-subtitle">{result.metrics.tokens.toLocaleString('pt-BR')} tokens estimados ({result.metrics.classification}), {result.metrics.words.toLocaleString('pt-BR')} palavras</div>}
    <div className="task-create-actions"><button type="submit" className="btn-primary" disabled={submitting}>{submitting ? 'importando…' : 'importar e criar tarefa'}</button></div>
  </form>
}
