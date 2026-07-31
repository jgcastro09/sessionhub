import type { ExecutorConfig, LogEntry, Metric, Pipeline, QueueItem, Schedule, Project } from './types'

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
	projects: () => get<Project[]>('/api/v2/projects'),
	executors: (projectId: string) => get<ExecutorConfig[]>(`/api/v2/projects/${encodeURIComponent(projectId)}/executors`),
	metrics: (projectId: string) => get<Metric>(`/api/v2/projects/${encodeURIComponent(projectId)}/metrics`),
	logs: (projectId: string, limit = 100) => get<LogEntry[]>(`/api/v2/projects/${encodeURIComponent(projectId)}/logs?limit=${limit}`),
	queue: (projectId: string) => get<QueueItem[]>(`/api/v2/projects/${encodeURIComponent(projectId)}/queue`),
	schedules: (projectId: string) => get<Schedule[]>(`/api/v2/projects/${encodeURIComponent(projectId)}/automations`),
	pipelines: (projectId: string) => get<Pipeline[]>(`/api/v2/projects/${encodeURIComponent(projectId)}/pipelines`),
}
