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

export type RegistryKind =
  | 'implementation'
  | 'header'
  | 'test'
  | 'tool'
  | 'component'
  | 'config'
  | 'docs'
  | 'script'
  | 'other'
export type RegistryReviewStatus = 'needs_review' | 'reviewed'
export type RegistryCriticality = '' | 'standard' | 'important' | 'critical'
export type RegistryStatus = 'active' | 'missing' | 'deprecated'

export interface RegistrySymbolRef {
  name: string
  line: number
}

export interface RegistryEntry {
  entry_id: string
  path: string
  filename: string
  extension: string
  language: string
  kind: RegistryKind
  category: string
  module: string
  area: string
  role: string
  symbols?: Record<string, RegistrySymbolRef[]>
  includes?: string[]
  imports?: string[]
  exports?: string[]
  dependencies?: string[]
  line_count: number
  size_bytes: number
  last_modified_at: string
  hash: string
  status: RegistryStatus
  description?: string
  responsibilities?: string[]
  criticality?: RegistryCriticality
  related_files?: string[]
  probable_related_files?: string[]
  previous_paths?: string[]
  last_reviewed_hash?: string
  review_status: RegistryReviewStatus
  reviewed_at?: string
  reviewed_by?: string
  last_scanned_at: string
  created_at: string
  updated_at: string
}

export interface RegistrySearchResult {
  entry: RegistryEntry
  score: number
  match_reasons?: string[]
}

export interface RegistrySearchPage {
  results: RegistrySearchResult[]
  total: number
}

export interface RegistrySchemaIssue {
  entry_id: string
  path: string
  field: string
  problem: string
}

export interface RegistryDependencyIssue {
  entry_id: string
  path: string
  raw_import: string
  reason: 'ambiguous' | 'unresolved'
}

export interface RegistryHealthReport {
  generated_at: string
  missing_paths: string[]
  orphaned_entries: string[]
  stale_reviews: string[]
  schema_issues: RegistrySchemaIssue[]
  classification_issues: string[]
  dangling_relationships: string[]
  dependency_issues: RegistryDependencyIssue[]
  pending_classification_count: number
  healthy: boolean
}

export interface RegistryStats {
  total_files: number
  total_lines: number
  by_category: Record<string, number>
  by_module: Record<string, number>
  by_area: Record<string, number>
  by_language: Record<string, number>
  by_kind: Record<string, number>
  by_review_status: Record<string, number>
  by_criticality: Record<string, number>
}

export interface RegistryPendingFile {
  path: string
  extension: string
  size_bytes: number
  detected_at: string
  suggested_reason?: string
}

export type RegistryReasons = Record<string, string>

export interface RegistryEligibilityPolicy {
  extension_reasons: RegistryReasons
  exact_filename_reasons: RegistryReasons
  explicit_include_reasons: RegistryReasons
  exclude_directory_reasons: RegistryReasons
  exclude_path_prefix_reasons: RegistryReasons
  exclude_file_reasons: RegistryReasons
  max_file_bytes?: number
}

export interface RegistryCategoryRule {
  path_prefixes?: string[]
  extensions?: string[]
  exact_filenames?: string[]
  exclude_extensions?: string[]
  fallback?: boolean
  category: string
}

export interface RegistryKindRule {
  path_globs?: string[]
  path_prefixes?: string[]
  extensions?: string[]
  exact_filenames?: string[]
  fallback?: boolean
  kind: RegistryKind
}

export interface RegistryClassificationPolicy {
  category_rules: RegistryCategoryRule[]
  kind_rules: RegistryKindRule[]
}

export interface RegistryConfig {
  roots?: string[]
  eligibility: RegistryEligibilityPolicy
  classification: RegistryClassificationPolicy
  module_overrides?: Record<string, string>
  search_aliases?: Record<string, string[]>
}

export interface RegistryMatchRule {
  path_globs?: string[]
  path_prefixes?: string[]
  module_prefixes?: string[]
  exact_paths?: string[]
  text_terms?: string[]
}

export interface RegistryRoleRule {
  id: string
  label: string
  match: RegistryMatchRule
}

export interface RegistryAreaNode {
  id: string
  label: string
  match: RegistryMatchRule
  children?: RegistryAreaNode[]
}

export interface RegistryTaxonomy {
  roles: RegistryRoleRule[]
  areas: RegistryAreaNode[]
}

export interface RegistryGraphNode {
  id: string
  kind: 'file' | 'area' | 'module'
  label: string
  path?: string
  category?: string
  review_status?: RegistryReviewStatus
  criticality?: RegistryCriticality
}

export interface RegistryGraphEdge {
  from: string
  to: string
  kind: 'contains' | 'implements' | 'imports' | 'includes' | 'related' | 'probable'
  confidence: 'confirmed' | 'resolved' | 'probable' | 'ambiguous'
}

export interface RegistryGraph {
  nodes: RegistryGraphNode[]
  edges: RegistryGraphEdge[]
}

export interface RegistryGraphWithIssues extends RegistryGraph {
  dependency_issues: RegistryDependencyIssue[]
}

export interface RegistryGitCorrelation {
  git: {
    repository: string
    branch: string
    head: string
    upstream?: string
    ahead: number
    behind: number
    clean: boolean
    conflicted: boolean
    files: { path: string; status: string; conflict: boolean }[]
  }
  changed_entries: string[]
}

export interface RegistryFileRevision {
  hash: string
  author: string
  date: string
  subject: string
}

export interface RegistrySearchQuery {
  query?: string
  category?: string
  module?: string
  area?: string
  language?: string
  role?: string
  review_status?: string
  criticality?: string
  kind?: string
  hash?: string
  semantic?: boolean
  limit?: number
  offset?: number
}

export interface RegistryReviewInput {
  description: string
  responsibilities: string[]
  criticality: RegistryCriticality
  related_files: string[]
  reviewed_by?: string
}
