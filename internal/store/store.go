package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jgcastro09/sessionhub/internal/domain"
	"github.com/jgcastro09/sessionhub/internal/id"
	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("not found")

type Store struct {
	db *sql.DB
}

func Open(ctx context.Context, path string) (*Store, error) {
	if path == "" {
		return nil, fmt.Errorf("database path is required")
	}
	if path != ":memory:" && !strings.HasPrefix(path, "file:") {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
	}
	dsn := path
	if path != ":memory:" && !strings.HasPrefix(path, "file:") {
		dsn = "file:" + filepath.ToSlash(path)
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open SQLite database: %w", err)
	}
	// WAL mode (enabled below) lets SQLite itself serialize writers via its
	// own file lock + busy_timeout, so the Go-level pool doesn't need to
	// funnel everything through a single connection. A cap of 1 here starved
	// latency-sensitive calls (e.g. starting a second terminal) behind a busy
	// terminal's continuous history-write traffic (each active PTY can emit
	// dozens of writes/sec via AppendTerminal), since a lone connection can
	// never be free for a competing query while that traffic keeps flowing.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err = db.ExecContext(ctx, `
PRAGMA foreign_keys = ON;
PRAGMA journal_mode = WAL;
PRAGMA synchronous = FULL;
PRAGMA busy_timeout = 5000;
PRAGMA wal_autocheckpoint = 500;`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("configure SQLite database: %w", err)
	}
	if err = applyMigrations(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func marshal(v any) ([]byte, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("encode persistent record: %w", err)
	}
	return data, nil
}

func decode[T any](data []byte) (T, error) {
	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		return out, fmt.Errorf("decode persistent record: %w", err)
	}
	return out, nil
}

func nowUTC() time.Time { return time.Now().UTC() }

// Recover converts process-backed running records to interrupted in one
// transaction. Completed records and effect receipts are never changed.
func (s *Store) Recover(ctx context.Context) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin recovery: %w", err)
	}
	defer tx.Rollback()
	now := nowUTC().Format(time.RFC3339Nano)
	var count int64
	for _, statement := range []string{
		`UPDATE instances SET state='interrupted', updated_at=? WHERE state IN ('running','waiting')`,
		`UPDATE queue_items SET state='interrupted', updated_at=? WHERE state='running'`,
		`UPDATE pipeline_steps SET state='interrupted', updated_at=? WHERE state='running'`,
		`UPDATE pipelines SET state='interrupted', updated_at=? WHERE state='running'`,
		`UPDATE automation_runs SET state='interrupted', updated_at=?, ended_at=? WHERE state='running'`,
	} {
		var result sql.Result
		if strings.Contains(statement, "automation_runs") {
			result, err = tx.ExecContext(ctx, statement, now, now)
		} else {
			result, err = tx.ExecContext(ctx, statement, now)
		}
		if err != nil {
			return 0, fmt.Errorf("mark recovery records: %w", err)
		}
		n, _ := result.RowsAffected()
		count += n
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM workspace_locks`); err != nil {
		return 0, fmt.Errorf("release stale workspace locks: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit recovery: %w", err)
	}
	return count, nil
}

func (s *Store) SaveExecutor(ctx context.Context, cfg domain.ExecutorConfig) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	now := nowUTC()
	if cfg.ID == "" {
		cfg.ID = id.New("exec")
	}
	if cfg.CreatedAt.IsZero() {
		cfg.CreatedAt = now
	}
	cfg.UpdatedAt = now
	data, err := marshal(cfg)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO executors(id,name,command,payload,created_at,updated_at)
VALUES(?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET
 name=excluded.name, command=excluded.command, payload=excluded.payload, updated_at=excluded.updated_at`,
		cfg.ID, cfg.Name, cfg.Command, data, cfg.CreatedAt.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("save executor %q: %w", cfg.Name, err)
	}
	return nil
}

func (s *Store) GetExecutor(ctx context.Context, executorID string) (domain.ExecutorConfig, error) {
	var data []byte
	err := s.db.QueryRowContext(ctx, `SELECT payload FROM executors WHERE id=?`, executorID).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ExecutorConfig{}, ErrNotFound
	}
	if err != nil {
		return domain.ExecutorConfig{}, fmt.Errorf("get executor: %w", err)
	}
	return decode[domain.ExecutorConfig](data)
}

func (s *Store) ListExecutors(ctx context.Context) ([]domain.ExecutorConfig, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT payload FROM executors ORDER BY lower(name)`)
	if err != nil {
		return nil, fmt.Errorf("list executors: %w", err)
	}
	defer rows.Close()
	var out []domain.ExecutorConfig
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, fmt.Errorf("scan executor: %w", err)
		}
		v, err := decode[domain.ExecutorConfig](data)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// DeleteExecutor removes an executor along with the instances and queue
// items that reference it (terminal_history cascades from instances).
// Executors are swappable configuration, not history: deleting one is not
// expected to be blocked by runs that already happened.
func (s *Store) DeleteExecutor(ctx context.Context, executorID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("delete executor: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM queue_items WHERE executor_id=?`, executorID); err != nil {
		return fmt.Errorf("delete executor: clear queue items: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM instances WHERE executor_id=?`, executorID); err != nil {
		return fmt.Errorf("delete executor: clear instances: %w", err)
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM executors WHERE id=?`, executorID)
	if err != nil {
		return fmt.Errorf("delete executor: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete executor: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

func (s *Store) SaveSession(ctx context.Context, session domain.Session) (domain.Session, error) {
	if err := session.Validate(); err != nil {
		return domain.Session{}, err
	}
	now := nowUTC()
	if session.ID == "" {
		session.ID = id.New("sess")
	}
	if session.CreatedAt.IsZero() {
		session.CreatedAt = now
	}
	session.UpdatedAt = now
	if len(session.Settings) == 0 {
		session.Settings = json.RawMessage(`{}`)
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO sessions(id,name,workspace,active_instance_id,settings,created_at,updated_at)
VALUES(?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET
 name=excluded.name, workspace=excluded.workspace,
 active_instance_id=excluded.active_instance_id, settings=excluded.settings,
 updated_at=excluded.updated_at`,
		session.ID, session.Name, session.Workspace, session.ActiveInstance,
		session.Settings, session.CreatedAt.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		return domain.Session{}, fmt.Errorf("save session %q: %w", session.Name, err)
	}
	return session, nil
}

func (s *Store) ListSessions(ctx context.Context) ([]domain.Session, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id,name,workspace,active_instance_id,settings,created_at,updated_at
FROM sessions ORDER BY updated_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()
	var out []domain.Session
	for rows.Next() {
		var v domain.Session
		var created, updated string
		if err := rows.Scan(&v.ID, &v.Name, &v.Workspace, &v.ActiveInstance,
			&v.Settings, &created, &updated); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		v.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		v.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) GetSession(ctx context.Context, sessionID string) (domain.Session, error) {
	var v domain.Session
	var created, updated string
	err := s.db.QueryRowContext(ctx, `
SELECT id,name,workspace,active_instance_id,settings,created_at,updated_at
FROM sessions WHERE id=?`, sessionID).Scan(&v.ID, &v.Name, &v.Workspace,
		&v.ActiveInstance, &v.Settings, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	if err != nil {
		return v, fmt.Errorf("get session: %w", err)
	}
	v.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	v.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return v, nil
}

// DeleteSession removes a session. Every other table that scopes to a
// session (instances, contexts, checkpoints, queue_items, schedules,
// pipelines, approvals, automation_runs, watchers, reports) references
// sessions(id) ON DELETE CASCADE, so this cleans up transitively.
func (s *Store) DeleteSession(ctx context.Context, sessionID string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id=?`, sessionID)
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) CreateInstance(ctx context.Context, v domain.Instance) (domain.Instance, error) {
	now := nowUTC()
	if v.ID == "" {
		v.ID = id.New("inst")
	}
	if v.State == "" {
		v.State = domain.StatePending
	}
	v.UpdatedAt = now
	data, err := marshal(v)
	if err != nil {
		return v, err
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO instances(id,session_id,executor_id,state,payload,updated_at)
VALUES(?,?,?,?,?,?)`, v.ID, v.SessionID, v.ExecutorID, v.State, data,
		now.Format(time.RFC3339Nano))
	if err != nil {
		return v, fmt.Errorf("create executor instance: %w", err)
	}
	return v, nil
}

func (s *Store) UpdateInstance(ctx context.Context, v domain.Instance) error {
	current, err := s.GetInstance(ctx, v.ID)
	if err != nil {
		return err
	}
	if current.State != v.State {
		if err := domain.ValidateTransition(current.State, v.State); err != nil {
			return err
		}
	}
	v.UpdatedAt = nowUTC()
	data, err := marshal(v)
	if err != nil {
		return err
	}
	var started, ended any
	if v.StartedAt != nil {
		started = v.StartedAt.Format(time.RFC3339Nano)
	}
	if v.EndedAt != nil {
		ended = v.EndedAt.Format(time.RFC3339Nano)
	}
	_, err = s.db.ExecContext(ctx, `
UPDATE instances SET state=?, payload=?, started_at=?, ended_at=?, updated_at=? WHERE id=?`,
		v.State, data, started, ended, v.UpdatedAt.Format(time.RFC3339Nano), v.ID)
	if err != nil {
		return fmt.Errorf("update executor instance: %w", err)
	}
	return nil
}

func (s *Store) GetInstance(ctx context.Context, instanceID string) (domain.Instance, error) {
	var data []byte
	var state domain.State
	err := s.db.QueryRowContext(ctx, `SELECT payload,state FROM instances WHERE id=?`, instanceID).Scan(&data, &state)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Instance{}, ErrNotFound
	}
	if err != nil {
		return domain.Instance{}, fmt.Errorf("get instance: %w", err)
	}
	value, err := decode[domain.Instance](data)
	value.State = state
	return value, err
}

func (s *Store) ListInstances(ctx context.Context, sessionID string) ([]domain.Instance, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT payload,state FROM instances WHERE session_id=? ORDER BY updated_at DESC`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list instances: %w", err)
	}
	defer rows.Close()
	var out []domain.Instance
	for rows.Next() {
		var data []byte
		var state domain.State
		if err := rows.Scan(&data, &state); err != nil {
			return nil, err
		}
		v, err := decode[domain.Instance](data)
		if err != nil {
			return nil, err
		}
		v.State = state
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) AppendTerminal(ctx context.Context, instanceID, direction string, data []byte) error {
	if direction != "input" && direction != "output" && direction != "system" {
		return fmt.Errorf("invalid terminal history direction %q", direction)
	}
	copyData := append([]byte(nil), data...)
	_, err := s.db.ExecContext(ctx, `
INSERT INTO terminal_history(instance_id,direction,data,created_at) VALUES(?,?,?,?)`,
		instanceID, direction, copyData, nowUTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("append terminal history: %w", err)
	}
	return nil
}

func (s *Store) SaveContext(ctx context.Context, value domain.GlobalContext) error {
	value.UpdatedAt = nowUTC()
	data, err := marshal(value)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO contexts(session_id,payload,updated_at) VALUES(?,?,?)
ON CONFLICT(session_id) DO UPDATE SET payload=excluded.payload, updated_at=excluded.updated_at`,
		value.SessionID, data, value.UpdatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("save global context: %w", err)
	}
	return nil
}

func (s *Store) LoadContext(ctx context.Context, sessionID string) (domain.GlobalContext, error) {
	var data []byte
	err := s.db.QueryRowContext(ctx, `SELECT payload FROM contexts WHERE session_id=?`, sessionID).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.GlobalContext{SessionID: sessionID}, nil
	}
	if err != nil {
		return domain.GlobalContext{}, fmt.Errorf("load global context: %w", err)
	}
	return decode[domain.GlobalContext](data)
}

func (s *Store) CreateCheckpoint(ctx context.Context, cp domain.Checkpoint) (domain.Checkpoint, error) {
	if cp.ID == "" {
		cp.ID = id.New("cp")
	}
	if cp.CreatedAt.IsZero() {
		cp.CreatedAt = nowUTC()
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO checkpoints(id,session_id,name,automatic,snapshot,created_at)
VALUES(?,?,?,?,?,?)`, cp.ID, cp.SessionID, cp.Name, cp.Automatic, cp.Snapshot,
		cp.CreatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return cp, fmt.Errorf("create checkpoint: %w", err)
	}
	return cp, nil
}

func (s *Store) SaveMetric(ctx context.Context, metric domain.Metric) error {
	if metric.ID == "" {
		metric.ID = id.New("metric")
	}
	if metric.CreatedAt.IsZero() {
		metric.CreatedAt = nowUTC()
	}
	data, err := marshal(metric)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO metrics(id,session_id,executor_id,instance_id,automation_id,pipeline_step_id,
 model,input_tokens,output_tokens,cache_read,cache_write,duration_ms,cost_micros_usd,
 precision,payload,created_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		metric.ID, nullString(metric.SessionID), nullString(metric.ExecutorID),
		nullString(metric.InstanceID), nullString(metric.AutomationID),
		nullString(metric.PipelineStepID), metric.Model, metric.InputTokens,
		metric.OutputTokens, metric.CacheRead, metric.CacheWrite, metric.Duration,
		metric.CostMicrosUSD, metric.Precision, data,
		metric.CreatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("save metric: %w", err)
	}
	return nil
}

func nullString(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func (s *Store) Log(ctx context.Context, entry domain.LogEntry) error {
	if entry.ID == "" {
		entry.ID = id.New("log")
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = nowUTC()
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO logs(id,session_id,level,kind,message,fields,created_at)
VALUES(?,?,?,?,?,?,?)`, entry.ID, nullString(entry.SessionID), entry.Level,
		entry.Kind, entry.Message, entry.Fields, entry.CreatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("write log entry: %w", err)
	}
	return nil
}

func (s *Store) SetSetting(ctx context.Context, key string, value any) error {
	data, err := marshal(value)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO settings(key,value,updated_at) VALUES(?,?,?)
ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
		key, data, nowUTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) GetSetting(ctx context.Context, key string, target any) error {
	var data []byte
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=?`, key).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("get setting %q: %w", key, err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("decode setting %q: %w", key, err)
	}
	return nil
}
