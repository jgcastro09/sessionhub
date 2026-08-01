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
  TaskImportResponse,
  RegistryEntry,
  RegistrySearchPage,
  RegistrySearchQuery,
  RegistrySearchResult,
  RegistryHealthReport,
  RegistryEnsureFreshResult,
  RegistryStats,
  RegistryPendingFile,
  RegistryConfig,
  RegistryTaxonomy,
  RegistryGraph,
  RegistryGraphWithIssues,
  RegistryGitCorrelation,
  RegistryFileRevision,
  RegistryReviewInput,
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
	importTask: (projectId: string, text: string) =>
		send<TaskImportResponse>('POST', `/api/v2/projects/${encodeURIComponent(projectId)}/tasks/import`, { text }),

	// --- Code Registry ---
	registryConfig: (projectId: string) => get<RegistryConfig>(`/api/v2/projects/${encodeURIComponent(projectId)}/registry/config`),
	saveRegistryConfig: (projectId: string, cfg: RegistryConfig) =>
		send<RegistryConfig>('PUT', `/api/v2/projects/${encodeURIComponent(projectId)}/registry/config`, cfg),
	registryTaxonomy: (projectId: string) =>
		get<RegistryTaxonomy>(`/api/v2/projects/${encodeURIComponent(projectId)}/registry/taxonomy`),
	saveRegistryTaxonomy: (projectId: string, tax: RegistryTaxonomy) =>
		send<RegistryTaxonomy>('PUT', `/api/v2/projects/${encodeURIComponent(projectId)}/registry/taxonomy`, tax),
	registryEntry: (projectId: string, entryId: string) =>
		get<RegistryEntry>(`/api/v2/projects/${encodeURIComponent(projectId)}/registry/entries/${encodeURIComponent(entryId)}`),
	registrySource: (projectId: string, entryId: string) =>
		get<{ content: string }>(`/api/v2/projects/${encodeURIComponent(projectId)}/registry/entries/${encodeURIComponent(entryId)}/source`),
	registrySourceHistory: (projectId: string, entryId: string) =>
		get<RegistryFileRevision[]>(
			`/api/v2/projects/${encodeURIComponent(projectId)}/registry/entries/${encodeURIComponent(entryId)}/source/history`,
		),
	registrySourceAtRevision: (projectId: string, entryId: string, ref: string) =>
		get<{ content: string }>(
			`/api/v2/projects/${encodeURIComponent(projectId)}/registry/entries/${encodeURIComponent(entryId)}/source/at?ref=${encodeURIComponent(ref)}`,
		),
	registryHealth: (projectId: string) =>
		get<RegistryHealthReport>(`/api/v2/projects/${encodeURIComponent(projectId)}/registry/health`),
	registryStats: (projectId: string) => get<RegistryStats>(`/api/v2/projects/${encodeURIComponent(projectId)}/registry/stats`),
	registryPending: (projectId: string) =>
		get<RegistryPendingFile[]>(`/api/v2/projects/${encodeURIComponent(projectId)}/registry/pending`),
	registryScan: (projectId: string) =>
		send<RegistryEntry[]>('POST', `/api/v2/projects/${encodeURIComponent(projectId)}/registry/scan`),
	registryEnsureFresh: (projectId: string) =>
		send<RegistryEnsureFreshResult>('POST', `/api/v2/projects/${encodeURIComponent(projectId)}/registry/ensure-fresh`),
	registryGitStatus: (projectId: string) =>
		get<RegistryGitCorrelation>(`/api/v2/projects/${encodeURIComponent(projectId)}/registry/git`),
	registryGraph: (projectId: string) =>
		get<RegistryGraphWithIssues>(`/api/v2/projects/${encodeURIComponent(projectId)}/registry/graph`),
	registryEntryGraph: (projectId: string, entryId: string, depth = 2, limit = 150) =>
		get<RegistryGraph>(
			`/api/v2/projects/${encodeURIComponent(projectId)}/registry/entries/${encodeURIComponent(entryId)}/graph?depth=${depth}&limit=${limit}`,
		),
	registryEntries: (projectId: string, q: RegistrySearchQuery = {}) =>
		get<RegistrySearchPage>(`/api/v2/projects/${encodeURIComponent(projectId)}/registry/entries?${searchQueryString(q)}`),
	registrySearch: (projectId: string, q: RegistrySearchQuery) =>
		get<RegistrySearchPage>(`/api/v2/projects/${encodeURIComponent(projectId)}/registry/search?${searchQueryString(q)}`),
	registryReviewQueue: (projectId: string, limit = 50, offset = 0) =>
		get<RegistrySearchPage>(
			`/api/v2/projects/${encodeURIComponent(projectId)}/registry/review-queue?limit=${limit}&offset=${offset}`,
		),
	reviewRegistryEntry: (projectId: string, entryId: string, input: RegistryReviewInput) =>
		send<RegistryEntry>(
			'POST',
			`/api/v2/projects/${encodeURIComponent(projectId)}/registry/entries/${encodeURIComponent(entryId)}/review`,
			input,
		),
}

function searchQueryString(q: RegistrySearchQuery): string {
	const params = new URLSearchParams()
	if (q.query) params.set('query', q.query)
	if (q.category) params.set('category', q.category)
	if (q.module) params.set('module', q.module)
	if (q.area) params.set('area', q.area)
	if (q.language) params.set('language', q.language)
	if (q.role) params.set('role', q.role)
	if (q.review_status) params.set('review_status', q.review_status)
	if (q.criticality) params.set('criticality', q.criticality)
	if (q.kind) params.set('kind', q.kind)
	if (q.hash) params.set('hash', q.hash)
	if (q.semantic) params.set('semantic', 'true')
	if (q.limit) params.set('limit', String(q.limit))
	if (q.offset) params.set('offset', String(q.offset))
	return params.toString()
}

// Re-exported so views that only need the score/reasons shape don't have to
// import RegistrySearchResult separately from RegistrySearchPage.
export type { RegistrySearchResult }
