import type { ExecutorConfig, ExecutorStatus, LogEntry, Metric, Pipeline, QueueItem, Schedule, Session } from './types'

export class UnauthorizedError extends Error {
  constructor() {
    super('unauthorized')
    this.name = 'UnauthorizedError'
  }
}

async function get<T>(path: string): Promise<T> {
  const response = await fetch(path, { credentials: 'include' })
  if (response.status === 401) {
    throw new UnauthorizedError()
  }
  if (!response.ok) {
    throw new Error(`${path}: ${response.status} ${await response.text()}`)
  }
  return response.json() as Promise<T>
}

export function pair(code: string): Promise<void> {
  return fetch('/api/pair', {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ code }),
  }).then((response) => {
    if (!response.ok) {
      throw new Error('invalid pairing code')
    }
  })
}

export const api = {
  sessions: () => get<Session[]>('/api/sessions'),
  executors: () => get<ExecutorConfig[]>('/api/executors'),
  executorStatuses: () => get<ExecutorStatus[]>('/api/executors/status'),
  metrics: (sessionId?: string) => get<Metric>(`/api/metrics${sessionId ? `?session_id=${encodeURIComponent(sessionId)}` : ''}`),
  logs: (sessionId?: string, limit = 100) =>
    get<LogEntry[]>(`/api/logs?limit=${limit}${sessionId ? `&session_id=${encodeURIComponent(sessionId)}` : ''}`),
  queue: (sessionId?: string) => get<QueueItem[]>(`/api/queue${sessionId ? `?session_id=${encodeURIComponent(sessionId)}` : ''}`),
  schedules: (sessionId?: string) => get<Schedule[]>(`/api/schedules${sessionId ? `?session_id=${encodeURIComponent(sessionId)}` : ''}`),
  pipelines: (sessionId?: string) => get<Pipeline[]>(`/api/pipelines${sessionId ? `?session_id=${encodeURIComponent(sessionId)}` : ''}`),
}
