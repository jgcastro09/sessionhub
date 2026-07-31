import type {
  ExecutorConfig,
  LogEntry,
  Metric,
  Pipeline,
  QueueItem,
  Schedule,
  Project,
  Task,
  TaskStatus,
  AuditReport,
  TaskClaim,
  RegistryEntry,
  RegistrySearchResult,
  RegistryCoverageReport,
  RegistryContextPack,
} from './types'

export class UnauthorizedError extends Error {
  constructor() {
    super('unauthorized')
    this.name = 'UnauthorizedError'
  }
}

export class NotFoundError extends Error {
  constructor(path: string) {
    super(`not found: ${path}`)
    this.name = 'NotFoundError'
  }
}

async function get<T>(path: string): Promise<T> {
  const response = await fetch(path, { credentials: 'include' })
  if (response.status === 401) {
    throw new UnauthorizedError()
  }
  if (response.status === 404) {
    throw new NotFoundError(path)
  }
  if (!response.ok) {
    throw new Error(`${path}: ${response.status} ${await response.text()}`)
  }
  return response.json() as Promise<T>
}

async function send<T>(method: string, path: string, body?: unknown): Promise<T> {
  const response = await fetch(path, {
    method,
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  if (response.status === 401) {
    throw new UnauthorizedError()
  }
  if (response.status === 404) {
    throw new NotFoundError(path)
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

	// --- Task Manager ---
	tasks: (projectId: string) => get<Task[]>(`/api/v2/projects/${encodeURIComponent(projectId)}/tasks`),
	task: (projectId: string, taskId: string) =>
		get<Task>(`/api/v2/projects/${encodeURIComponent(projectId)}/tasks/${encodeURIComponent(taskId)}`),
	createTask: (
		projectId: string,
		input: { title: string; type: string; priority: string; impacted_areas?: string[]; registry_refs?: string[]; dependencies?: string[] },
	) => send<Task>('POST', `/api/v2/projects/${encodeURIComponent(projectId)}/tasks`, input),
	patchTask: (
		projectId: string,
		taskId: string,
		patch: Partial<{
			title: string
			type: string
			priority: string
			impacted_areas: string[]
			registry_refs: string[]
			dependencies: string[]
			sections: Record<string, string>
			status: TaskStatus
		}>,
	) => send<Task>('PATCH', `/api/v2/projects/${encodeURIComponent(projectId)}/tasks/${encodeURIComponent(taskId)}`, patch),
	auditTask: (projectId: string, taskId: string) =>
		send<AuditReport>('POST', `/api/v2/projects/${encodeURIComponent(projectId)}/tasks/${encodeURIComponent(taskId)}/audit`),
	taskClaims: (projectId: string) => get<TaskClaim[]>(`/api/v2/projects/${encodeURIComponent(projectId)}/tasks/claims`),

	// --- Code Registry ---
	registryEntries: (projectId: string) => get<RegistryEntry[]>(`/api/v2/projects/${encodeURIComponent(projectId)}/registry/entries`),
	registryEntry: (projectId: string, entryId: string) =>
		get<RegistryEntry>(`/api/v2/projects/${encodeURIComponent(projectId)}/registry/entries/${encodeURIComponent(entryId)}`),
	registrySource: (projectId: string, entryId: string) =>
		get<{ content: string }>(`/api/v2/projects/${encodeURIComponent(projectId)}/registry/entries/${encodeURIComponent(entryId)}/source`),
	registryHealth: (projectId: string) =>
		get<RegistryCoverageReport>(`/api/v2/projects/${encodeURIComponent(projectId)}/registry/health`),
	registryScan: (projectId: string) =>
		send<RegistryEntry[]>('POST', `/api/v2/projects/${encodeURIComponent(projectId)}/registry/scan`),
	registrySearch: (projectId: string, query: string) =>
		get<RegistrySearchResult[]>(
			`/api/v2/projects/${encodeURIComponent(projectId)}/registry/search?query=${encodeURIComponent(query)}`,
		),
	registryContext: (projectId: string, entryId: string) =>
		get<RegistryContextPack>(
			`/api/v2/projects/${encodeURIComponent(projectId)}/registry/context?entry_id=${encodeURIComponent(entryId)}`,
		),
	reviewRegistryEntry: (
		projectId: string,
		entryId: string,
		input: {
			module: string
			description: string
			responsibilities: string[]
			criticality: string
			relations_confirmed: string[]
			relations_probable: string[]
		},
	) =>
		send<RegistryEntry>(
			'POST',
			`/api/v2/projects/${encodeURIComponent(projectId)}/registry/entries/${encodeURIComponent(entryId)}/review`,
			input,
		),
}
