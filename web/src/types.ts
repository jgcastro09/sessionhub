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

export interface Session {
  id: string
  name: string
  workspace: string
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
  session_id?: string
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
  session_id?: string
  level: string
  kind: string
  message: string
  created_at: string
}

export interface QueueItem {
  id: string
  session_id: string
  executor_id: string
  prompt: string
  priority: number
  state: State
  created_at: string
}

export interface Schedule {
  id: string
  session_id: string
  name: string
  kind: string
  spec: string
  enabled: boolean
  next_run?: string
  last_run?: string
}

export interface Pipeline {
  id: string
  session_id: string
  name: string
  state: State
  started_at?: string
  ended_at?: string
  created_at: string
}
