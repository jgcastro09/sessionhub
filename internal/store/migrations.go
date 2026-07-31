package store

import (
	"context"
	"database/sql"
	"fmt"
)

type migration struct {
	version int
	sql     string
}

var migrations = []migration{
	{
		version: 1,
		sql: `
CREATE TABLE IF NOT EXISTS executors (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    command TEXT NOT NULL,
    payload BLOB NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS executors_name_ci ON executors(lower(name));

CREATE TABLE IF NOT EXISTS projects (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    root TEXT NOT NULL,
    active_instance_id TEXT NOT NULL DEFAULT '',
    settings BLOB NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS instances (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    executor_id TEXT NOT NULL REFERENCES executors(id) ON DELETE RESTRICT,
    state TEXT NOT NULL,
    payload BLOB NOT NULL,
    started_at TEXT,
    ended_at TEXT,
    updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS instances_project_state ON instances(project_id, state);

CREATE TABLE IF NOT EXISTS terminal_history (
    sequence INTEGER PRIMARY KEY AUTOINCREMENT,
    instance_id TEXT NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
    direction TEXT NOT NULL CHECK(direction IN ('input','output','system')),
    data BLOB NOT NULL,
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS terminal_history_instance_seq ON terminal_history(instance_id, sequence);

CREATE TABLE IF NOT EXISTS contexts (
    project_id TEXT PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
    payload BLOB NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS checkpoints (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    automatic INTEGER NOT NULL,
    snapshot BLOB NOT NULL,
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS checkpoints_project_created ON checkpoints(project_id, created_at DESC);

CREATE TABLE IF NOT EXISTS prices (
    id TEXT PRIMARY KEY,
    model TEXT NOT NULL,
    version TEXT NOT NULL,
    payload BLOB NOT NULL,
    effective_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS metrics (
    id TEXT PRIMARY KEY,
    project_id TEXT,
    executor_id TEXT,
    instance_id TEXT,
    automation_id TEXT,
    pipeline_step_id TEXT,
    model TEXT NOT NULL DEFAULT '',
    input_tokens INTEGER NOT NULL,
    output_tokens INTEGER NOT NULL,
    cache_read INTEGER NOT NULL,
    cache_write INTEGER NOT NULL,
    duration_ms INTEGER NOT NULL,
    cost_micros_usd INTEGER NOT NULL,
    precision TEXT NOT NULL,
    payload BLOB NOT NULL,
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS metrics_scope ON metrics(project_id, executor_id, created_at);

CREATE TABLE IF NOT EXISTS queue_items (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    executor_id TEXT NOT NULL REFERENCES executors(id) ON DELETE RESTRICT,
    state TEXT NOT NULL,
    priority INTEGER NOT NULL,
    idempotency_key TEXT NOT NULL UNIQUE,
    payload BLOB NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS queue_eligibility ON queue_items(project_id, state, priority DESC, created_at);

CREATE TABLE IF NOT EXISTS schedules (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    enabled INTEGER NOT NULL,
    next_run TEXT,
    payload BLOB NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS schedules_due ON schedules(enabled, next_run);

CREATE TABLE IF NOT EXISTS pipelines (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    state TEXT NOT NULL,
    template INTEGER NOT NULL,
    payload BLOB NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS pipeline_steps (
    id TEXT PRIMARY KEY,
    pipeline_id TEXT NOT NULL REFERENCES pipelines(id) ON DELETE CASCADE,
    state TEXT NOT NULL,
    step_type TEXT NOT NULL,
    idempotency_key TEXT NOT NULL UNIQUE,
    payload BLOB NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS pipeline_steps_ready ON pipeline_steps(pipeline_id, state);

CREATE TABLE IF NOT EXISTS step_dependencies (
    step_id TEXT NOT NULL REFERENCES pipeline_steps(id) ON DELETE CASCADE,
    dependency_id TEXT NOT NULL REFERENCES pipeline_steps(id) ON DELETE CASCADE,
    PRIMARY KEY(step_id, dependency_id),
    CHECK(step_id <> dependency_id)
);

CREATE TABLE IF NOT EXISTS approvals (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    target_type TEXT NOT NULL,
    target_id TEXT NOT NULL,
    state TEXT NOT NULL,
    payload BLOB NOT NULL,
    requested_at TEXT NOT NULL,
    decided_at TEXT
);
CREATE INDEX IF NOT EXISTS approvals_pending ON approvals(state, requested_at);

CREATE TABLE IF NOT EXISTS automation_runs (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    automation_type TEXT NOT NULL,
    automation_id TEXT NOT NULL,
    state TEXT NOT NULL,
    idempotency_key TEXT NOT NULL UNIQUE,
    payload BLOB NOT NULL,
    started_at TEXT,
    ended_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS effect_receipts (
    idempotency_key TEXT PRIMARY KEY,
    target_type TEXT NOT NULL,
    target_id TEXT NOT NULL,
    state TEXT NOT NULL,
    result BLOB,
    created_at TEXT NOT NULL,
    completed_at TEXT
);

CREATE TABLE IF NOT EXISTS workspace_locks (
    workspace TEXT NOT NULL,
    owner_id TEXT NOT NULL,
    mode TEXT NOT NULL CHECK(mode IN ('read','write')),
    files BLOB NOT NULL,
    acquired_at TEXT NOT NULL,
    PRIMARY KEY(workspace, owner_id)
);

CREATE TABLE IF NOT EXISTS watchers (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    enabled INTEGER NOT NULL,
    workspace TEXT NOT NULL,
    last_event_hash TEXT NOT NULL DEFAULT '',
    payload BLOB NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS logs (
    sequence INTEGER PRIMARY KEY AUTOINCREMENT,
    id TEXT NOT NULL UNIQUE,
    project_id TEXT,
    level TEXT NOT NULL,
    kind TEXT NOT NULL,
    message TEXT NOT NULL,
    fields BLOB,
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS logs_project_created ON logs(project_id, created_at DESC);

CREATE TABLE IF NOT EXISTS reports (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    target_type TEXT NOT NULL,
    target_id TEXT NOT NULL,
    payload BLOB NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS settings (
    key TEXT PRIMARY KEY,
    value BLOB NOT NULL,
    updated_at TEXT NOT NULL
);
`,
	},
}

func applyMigrations(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL
)`); err != nil {
		return fmt.Errorf("create schema migration table: %w", err)
	}

	var current int
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&current); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	for _, m := range migrations {
		if m.version <= current {
			continue
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %d: %w", m.version, err)
		}
		if _, err = tx.ExecContext(ctx, m.sql); err == nil {
			_, err = tx.ExecContext(ctx,
				`INSERT INTO schema_migrations(version, applied_at) VALUES(?, strftime('%Y-%m-%dT%H:%M:%fZ','now'))`,
				m.version)
		}
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %d: %w", m.version, err)
		}
		if err = tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %d: %w", m.version, err)
		}
		current = m.version
	}
	return nil
}
