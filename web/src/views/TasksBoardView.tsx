import { useState } from 'react'
import { api } from '../api'
import { usePoll } from '../hooks/usePoll'
import { Link } from '../router'
import type { Task, TaskStatus } from '../types'

const COLUMNS: { id: TaskStatus; label: string }[] = [
  { id: 'idea', label: 'Ideia' },
  { id: 'backlog', label: 'Backlog' },
  { id: 'ready', label: 'Pronta' },
  { id: 'in_progress', label: 'Em andamento' },
  { id: 'changes_requested', label: 'Ajustes solicitados' },
  { id: 'done', label: 'Concluída' },
  { id: 'archived', label: 'Arquivada' },
]

const PRIORITY_LABEL: Record<string, string> = { low: 'Baixa', medium: 'Média', high: 'Alta', urgent: 'Urgente' }

export function TasksBoardView({ projectId, tick, onUnauthorized }: { projectId: string; tick: number; onUnauthorized: () => void }) {
  const { data: tasks, error, loading } = usePoll(() => api.tasks(projectId), 10000, onUnauthorized, [projectId, tick])
  const [actionError, setActionError] = useState<string>()
  const [creating, setCreating] = useState(false)
  const [dragTaskId, setDragTaskId] = useState<string>()
  const [refreshNonce, setRefreshNonce] = useState(0)
  const [localTasks, setLocalTasks] = useState<Task[] | undefined>(undefined)

  const board = localTasks ?? tasks

  async function handleDrop(status: TaskStatus) {
    if (!dragTaskId) return
    const taskId = dragTaskId
    setDragTaskId(undefined)
    setActionError(undefined)
    // Optimistic move; a rejected transition (invalid workflow edge) reverts
    // on the next poll since we don't touch localTasks after the request settles.
    setLocalTasks((current) => (current ?? tasks ?? []).map((t) => (t.id === taskId ? { ...t, status } : t)))
    try {
      await api.patchTask(projectId, taskId, { status })
      setRefreshNonce((n) => n + 1)
    } catch (err) {
      setActionError(err instanceof Error ? err.message : String(err))
      setLocalTasks(undefined)
    }
  }

  return (
    <>
      <div className="view-title-row">
        <div className="view-title">Tarefas</div>
        <button type="button" className="btn-primary" onClick={() => setCreating((v) => !v)}>
          {creating ? 'cancelar' : '+ nova tarefa'}
        </button>
      </div>
      {error && <div className="error-banner">{error}</div>}
      {actionError && <div className="error-banner">{actionError}</div>}
      {creating && (
        <CreateTaskForm
          projectId={projectId}
          onCreated={() => {
            setCreating(false)
            setLocalTasks(undefined)
            setRefreshNonce((n) => n + 1)
          }}
        />
      )}
      {!loading && board?.length === 0 && (
        <div className="empty-state">
          <div className="prompt">$</div>
          Nenhuma tarefa ainda.
        </div>
      )}
      <div className="board" key={refreshNonce}>
        {COLUMNS.map((column) => (
          <div
            className="board-column"
            key={column.id}
            onDragOver={(e) => e.preventDefault()}
            onDrop={() => handleDrop(column.id)}
          >
            <div className="board-column-header">
              {column.label}
              <span className="board-count">{board?.filter((t) => t.status === column.id).length ?? 0}</span>
            </div>
            <div className="board-column-body">
              {board
                ?.filter((t) => t.status === column.id)
                .map((task) => (
                  <div
                    className="board-card"
                    key={task.id}
                    draggable
                    onDragStart={() => setDragTaskId(task.id)}
                  >
                    <Link to={`/projects/${projectId}/tasks/${task.id}`} className="board-card-link">
                      <div className="board-card-id">{task.id}</div>
                      <div className="board-card-title">{task.title}</div>
                      <div className="board-card-meta">
                        <span className={`priority priority-${task.priority}`}>{PRIORITY_LABEL[task.priority] ?? task.priority}</span>
                      </div>
                    </Link>
                  </div>
                ))}
            </div>
          </div>
        ))}
      </div>
    </>
  )
}

function CreateTaskForm({ projectId, onCreated }: { projectId: string; onCreated: () => void }) {
  const [title, setTitle] = useState('')
  const [type, setType] = useState('feature')
  const [priority, setPriority] = useState('medium')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string>()

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    if (!title.trim()) return
    setSubmitting(true)
    setError(undefined)
    try {
      await api.createTask(projectId, { title, type, priority })
      onCreated()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <form className="inline-form" onSubmit={submit}>
      {error && <div className="error-banner">{error}</div>}
      <input
        className="text-input"
        placeholder="título da tarefa"
        value={title}
        onChange={(e) => setTitle(e.target.value)}
        autoFocus
      />
      <select className="text-input" value={type} onChange={(e) => setType(e.target.value)}>
        <option value="feature">feature</option>
        <option value="bug">bug</option>
        <option value="chore">chore</option>
        <option value="spike">spike</option>
      </select>
      <select className="text-input" value={priority} onChange={(e) => setPriority(e.target.value)}>
        <option value="low">baixa</option>
        <option value="medium">média</option>
        <option value="high">alta</option>
        <option value="urgent">urgente</option>
      </select>
      <button type="submit" className="btn-primary" disabled={submitting}>
        criar
      </button>
    </form>
  )
}
