// Mirrors the JSON shapes internal/webserver's handlers encode from
// internal/domain (see internal/webserver/api.go). Kept intentionally
// narrow to what the monitoring views in v1 read.

export type State =
  | 'pending'
  | 'queued'
  | 'running'
  | 'succeeded'
  | 'failed'
  | 'canceled'
  | 'waiting_approval'
  | string

export interface Project {
  id: string
  name: string
	root: string
  active_instance?: string
  created_at: string
  updated_at: string
}

export interface ExecutorConfig {
  id: string
  name: string
  command: string
  model?: string
  roles?: string[]
}

export interface ExecutorStatus {
  executor_id: string
  login_known: boolean
  activated: boolean
  live: boolean
}

export interface Metric {
  project_id?: string
  executor_id?: string
  input_tokens: number
  output_tokens: number
  cache_read: number
  cache_write: number
  duration_ms: number
  cost_micros_usd: number
  precision?: string
}

export interface LogEntry {
  id: string
  project_id?: string
  level: string
  kind: string
  message: string
  created_at: string
}

export interface QueueItem {
  id: string
  project_id: string
  executor_id: string
  prompt: string
  priority: number
  state: State
  created_at: string
}

export interface Schedule {
  id: string
  project_id: string
  name: string
  kind: string
  spec: string
  enabled: boolean
  next_run?: string
  last_run?: string
}

export interface Pipeline {
  id: string
  project_id: string
  name: string
  state: State
  started_at?: string
  ended_at?: string
  created_at: string
}

// --- Task Manager (internal/tasks) ---

export type TaskStatus = 'idea' | 'backlog' | 'ready' | 'in_progress' | 'changes_requested' | 'done' | 'archived'
export type TaskPriority = 'low' | 'medium' | 'high' | 'urgent'

export interface TaskSection {
  heading: string
  body: string
}

export interface Task {
  id: string
  title: string
  type: string
  status: TaskStatus
  priority: TaskPriority
  created_at: string
  updated_at: string
  impacted_areas: string[]
  registry_refs: string[]
  dependencies: string[]
  sections: TaskSection[]
}

export interface AuditCheck {
  kind: 'registry' | 'source' | 'validation'
  raw: string
}

export interface AuditCheckResult {
  check: AuditCheck
  passed: boolean
  resolved: boolean
  detail: string
}

export interface AuditReport {
  ran_at: string
  results: AuditCheckResult[]
  reproducible_pass: boolean
}

export interface TaskClaim {
  task_id: string
  executor_id: string
  terminal_id: string
  claimed_at: string
}

// --- Code Registry (internal/registry) ---

export interface RegistryEntry {
  entry_id: string
  path: string
  category: string
  language: string
  hash: string
  size: number
  lines: number
  symbols?: string[]
  status: 'active' | 'missing'
  module?: string
  description?: string
  responsibilities?: string[]
  criticality?: string
  relations_confirmed?: string[]
  relations_probable?: string[]
  reviewed: boolean
  reviewed_at?: string
  created_at: string
  updated_at: string
}

export interface RegistrySearchResult {
  entry: RegistryEntry
  score: number
}

export interface RegistryCoverageReport {
  missing_paths: string[]
  stale_hashes: string[]
}

export interface RegistryContextPack {
  entry: RegistryEntry
  related: RegistrySearchResult[]
}
