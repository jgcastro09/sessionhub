package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jgcastro09/sessionhub/internal/domain"
	"github.com/jgcastro09/sessionhub/internal/id"
)

func (s *Store) Enqueue(ctx context.Context, item domain.QueueItem) (domain.QueueItem, error) {
	now := nowUTC()
	if item.ID == "" {
		item.ID = id.New("queue")
	}
	if item.IdempotencyKey == "" {
		item.IdempotencyKey = "queue:" + item.ID
	}
	if item.State == "" {
		item.State = domain.StatePending
	}
	if item.MaxAttempts <= 0 {
		item.MaxAttempts = 1
	}
	item.CreatedAt, item.UpdatedAt = now, now
	data, err := marshal(item)
	if err != nil {
		return item, err
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO queue_items(id,project_id,executor_id,state,priority,idempotency_key,payload,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?,?)`, item.ID, item.ProjectID, item.ExecutorID, item.State,
		item.Priority, item.IdempotencyKey, data, now.Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano))
	if err != nil {
		return item, fmt.Errorf("enqueue prompt: %w", err)
	}
	return item, nil
}

func (s *Store) ListQueue(ctx context.Context, projectID string) ([]domain.QueueItem, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT payload,state FROM queue_items WHERE project_id=?
ORDER BY priority DESC, created_at`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list queue: %w", err)
	}
	defer rows.Close()
	var out []domain.QueueItem
	for rows.Next() {
		var data []byte
		var state domain.State
		if err := rows.Scan(&data, &state); err != nil {
			return nil, err
		}
		item, err := decode[domain.QueueItem](data)
		if err != nil {
			return nil, err
		}
		item.State = state
		out = append(out, item)
	}
	return out, rows.Err()
}

// ClaimNextQueue atomically claims one eligible pending item and reserves its
// idempotency key before the caller writes to a PTY.
func (s *Store) ClaimNextQueue(ctx context.Context, projectID string) (*domain.QueueItem, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin queue claim: %w", err)
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `
SELECT payload FROM queue_items
WHERE project_id=? AND state='pending'
ORDER BY priority DESC, created_at`, projectID)
	if err != nil {
		return nil, fmt.Errorf("read queue candidates: %w", err)
	}
	var candidates []domain.QueueItem
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			rows.Close()
			return nil, err
		}
		item, err := decode[domain.QueueItem](data)
		if err != nil {
			rows.Close()
			return nil, err
		}
		candidates = append(candidates, item)
	}
	rows.Close()

	var chosen *domain.QueueItem
	for i := range candidates {
		eligible := true
		for _, dependency := range candidates[i].Dependencies {
			var state domain.State
			err := tx.QueryRowContext(ctx, `SELECT state FROM queue_items WHERE id=?`, dependency).Scan(&state)
			if err != nil || state != domain.StateSucceeded {
				eligible = false
				break
			}
		}
		if eligible {
			chosen = &candidates[i]
			break
		}
	}
	if chosen == nil {
		return nil, nil
	}
	now := nowUTC()
	chosen.Attempts++
	chosen.State = domain.StateRunning
	chosen.UpdatedAt = now
	chosenData, err := marshal(chosen)
	if err != nil {
		return nil, err
	}
	result, err := tx.ExecContext(ctx, `
UPDATE queue_items SET state='running', payload=?, updated_at=?
WHERE id=? AND state='pending'`,
		chosenData, now.Format(time.RFC3339Nano), chosen.ID)
	if err != nil {
		return nil, fmt.Errorf("claim queue item: %w", err)
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return nil, nil
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO effect_receipts(idempotency_key,target_type,target_id,state,created_at)
VALUES(?, 'queue_item', ?, 'running', ?)`,
		fmt.Sprintf("%s:attempt:%d", chosen.IdempotencyKey, chosen.Attempts),
		chosen.ID, now.Format(time.RFC3339Nano))
	if err != nil {
		if isUniqueViolation(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reserve queue effect: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit queue claim: %w", err)
	}
	return chosen, nil
}

func isUniqueViolation(err error) bool {
	return err != nil && (contains(err.Error(), "UNIQUE constraint failed") ||
		contains(err.Error(), "constraint failed"))
}

func contains(value, part string) bool {
	for i := 0; i+len(part) <= len(value); i++ {
		if value[i:i+len(part)] == part {
			return true
		}
	}
	return false
}

func (s *Store) CompleteQueue(
	ctx context.Context,
	itemID string,
	outcome domain.State,
	ruleID string,
	result json.RawMessage,
	message string,
) error {
	if outcome != domain.StateSucceeded && outcome != domain.StateFailed &&
		outcome != domain.StateCanceled && outcome != domain.StateWaiting {
		return fmt.Errorf("invalid queue completion outcome %q", outcome)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var data []byte
	var current domain.State
	err = tx.QueryRowContext(ctx, `
SELECT payload,state FROM queue_items WHERE id=?`, itemID).
		Scan(&data, &current)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("read queue completion target: %w", err)
	}
	item, err := decode[domain.QueueItem](data)
	if err != nil {
		return err
	}
	effectOutcome := outcome
	if outcome == domain.StateFailed && item.Attempts < item.MaxAttempts &&
		item.FailurePolicy == "retry" {
		outcome = domain.StatePending
	}
	if err := domain.ValidateTransition(current, outcome); err != nil {
		return err
	}
	item.State, item.CompletionRuleID = outcome, ruleID
	item.Result, item.Error = result, message
	item.UpdatedAt = nowUTC()
	updated, err := marshal(item)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
UPDATE queue_items SET state=?,payload=?,updated_at=? WHERE id=?`,
		outcome, updated, item.UpdatedAt.Format(time.RFC3339Nano), itemID)
	if err == nil {
		_, err = tx.ExecContext(ctx, `
UPDATE effect_receipts SET state=?,result=?,completed_at=?
WHERE target_type='queue_item' AND target_id=? AND state='running'`,
			effectOutcome, result, item.UpdatedAt.Format(time.RFC3339Nano), itemID)
	}
	if err != nil {
		return fmt.Errorf("complete queue item: %w", err)
	}
	return tx.Commit()
}

func (s *Store) SaveSchedule(ctx context.Context, schedule domain.Schedule) (domain.Schedule, error) {
	now := nowUTC()
	if schedule.ID == "" {
		schedule.ID = id.New("sched")
	}
	if schedule.CreatedAt.IsZero() {
		schedule.CreatedAt = now
	}
	schedule.UpdatedAt = now
	data, err := marshal(schedule)
	if err != nil {
		return schedule, err
	}
	var next any
	if schedule.NextRun != nil {
		next = schedule.NextRun.UTC().Format(time.RFC3339Nano)
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO schedules(id,project_id,enabled,next_run,payload,created_at,updated_at)
VALUES(?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET enabled=excluded.enabled,next_run=excluded.next_run,
 payload=excluded.payload,updated_at=excluded.updated_at`,
		schedule.ID, schedule.ProjectID, schedule.Enabled, next, data,
		schedule.CreatedAt.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		return schedule, fmt.Errorf("save schedule: %w", err)
	}
	return schedule, nil
}

func (s *Store) DueSchedules(ctx context.Context, at time.Time) ([]domain.Schedule, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT payload FROM schedules WHERE enabled=1 AND next_run IS NOT NULL AND next_run<=?
ORDER BY next_run`, at.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, fmt.Errorf("read due schedules: %w", err)
	}
	defer rows.Close()
	var out []domain.Schedule
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		v, err := decode[domain.Schedule](data)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) ListSchedules(ctx context.Context, projectID string) ([]domain.Schedule, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT payload FROM schedules WHERE project_id=?
ORDER BY created_at`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list schedules: %w", err)
	}
	defer rows.Close()
	var out []domain.Schedule
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		v, err := decode[domain.Schedule](data)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func ValidatePipeline(steps []domain.PipelineStep) error {
	byID := make(map[string]domain.PipelineStep, len(steps))
	for _, step := range steps {
		if step.ID == "" {
			return fmt.Errorf("pipeline step %q has no id", step.Name)
		}
		if _, exists := byID[step.ID]; exists {
			return fmt.Errorf("duplicate pipeline step id %q", step.ID)
		}
		if step.MaxAttempts <= 0 {
			return fmt.Errorf("pipeline step %q must allow at least one attempt", step.Name)
		}
		byID[step.ID] = step
	}
	visiting, visited := map[string]bool{}, map[string]bool{}
	var walk func(string) error
	walk = func(stepID string) error {
		if visiting[stepID] {
			return fmt.Errorf("pipeline dependency cycle at %q", stepID)
		}
		if visited[stepID] {
			return nil
		}
		visiting[stepID] = true
		for _, dependency := range byID[stepID].Dependencies {
			if _, ok := byID[dependency]; !ok {
				return fmt.Errorf("step %q depends on missing step %q", stepID, dependency)
			}
			if err := walk(dependency); err != nil {
				return err
			}
		}
		delete(visiting, stepID)
		visited[stepID] = true
		return nil
	}
	for stepID := range byID {
		if err := walk(stepID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) SavePipeline(
	ctx context.Context,
	pipeline domain.Pipeline,
	steps []domain.PipelineStep,
) (domain.Pipeline, []domain.PipelineStep, error) {
	now := nowUTC()
	if pipeline.ID == "" {
		pipeline.ID = id.New("pipe")
	}
	if pipeline.State == "" {
		pipeline.State = domain.StatePending
	}
	if pipeline.CreatedAt.IsZero() {
		pipeline.CreatedAt = now
	}
	pipeline.UpdatedAt = now
	for i := range steps {
		if steps[i].ID == "" {
			steps[i].ID = id.New("step")
		}
		steps[i].PipelineID = pipeline.ID
		if steps[i].State == "" {
			steps[i].State = domain.StatePending
		}
		if steps[i].MaxAttempts <= 0 {
			steps[i].MaxAttempts = 1
		}
		if steps[i].IdempotencyKey == "" {
			steps[i].IdempotencyKey = "pipeline:" + pipeline.ID + ":" + steps[i].ID
		}
		steps[i].CreatedAt, steps[i].UpdatedAt = now, now
	}
	if err := ValidatePipeline(steps); err != nil {
		return pipeline, steps, err
	}
	pipelineData, err := marshal(pipeline)
	if err != nil {
		return pipeline, steps, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return pipeline, steps, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `
INSERT INTO pipelines(id,project_id,name,state,template,payload,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?)`,
		pipeline.ID, pipeline.ProjectID, pipeline.Name, pipeline.State,
		pipeline.Template, pipelineData, pipeline.CreatedAt.Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano))
	if err != nil {
		return pipeline, steps, fmt.Errorf("save pipeline: %w", err)
	}
	for _, step := range steps {
		data, err := marshal(step)
		if err != nil {
			return pipeline, steps, err
		}
		_, err = tx.ExecContext(ctx, `
INSERT INTO pipeline_steps(id,pipeline_id,state,step_type,idempotency_key,payload,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?)`,
			step.ID, pipeline.ID, step.State, step.Type, step.IdempotencyKey,
			data, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
		if err != nil {
			return pipeline, steps, fmt.Errorf("save pipeline step %q: %w", step.Name, err)
		}
		for _, dependency := range step.Dependencies {
			if _, err = tx.ExecContext(ctx, `
INSERT INTO step_dependencies(step_id,dependency_id) VALUES(?,?)`,
				step.ID, dependency); err != nil {
				return pipeline, steps, fmt.Errorf("save step dependency: %w", err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return pipeline, steps, err
	}
	return pipeline, steps, nil
}

func (s *Store) ListPipelines(ctx context.Context, projectID string) ([]domain.Pipeline, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT payload FROM pipelines WHERE project_id=?
ORDER BY created_at`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list pipelines: %w", err)
	}
	defer rows.Close()
	var out []domain.Pipeline
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		v, err := decode[domain.Pipeline](data)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) ClaimReadySteps(ctx context.Context, pipelineID string, limit int) ([]domain.PipelineStep, error) {
	if limit <= 0 {
		return nil, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `
SELECT ps.payload
FROM pipeline_steps ps
WHERE ps.pipeline_id=? AND ps.state='pending'
AND NOT EXISTS (
  SELECT 1 FROM step_dependencies d
  JOIN pipeline_steps dependency ON dependency.id=d.dependency_id
  WHERE d.step_id=ps.id AND dependency.state<>'succeeded'
)
ORDER BY ps.created_at
LIMIT ?`, pipelineID, limit)
	if err != nil {
		return nil, err
	}
	var candidates []domain.PipelineStep
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			rows.Close()
			return nil, err
		}
		step, err := decode[domain.PipelineStep](data)
		if err != nil {
			rows.Close()
			return nil, err
		}
		candidates = append(candidates, step)
	}
	rows.Close()
	now := nowUTC()
	var claimed []domain.PipelineStep
	for _, step := range candidates {
		step.State, step.UpdatedAt = domain.StateRunning, now
		step.Attempts++
		stepData, err := marshal(step)
		if err != nil {
			return nil, err
		}
		result, err := tx.ExecContext(ctx, `
UPDATE pipeline_steps SET state='running',payload=?,updated_at=? WHERE id=? AND state='pending'`,
			stepData, now.Format(time.RFC3339Nano), step.ID)
		if err != nil {
			return nil, err
		}
		if n, _ := result.RowsAffected(); n != 1 {
			continue
		}
		_, err = tx.ExecContext(ctx, `
INSERT INTO effect_receipts(idempotency_key,target_type,target_id,state,created_at)
VALUES(?,'pipeline_step',?,'running',?)`,
			fmt.Sprintf("%s:attempt:%d", step.IdempotencyKey, step.Attempts),
			step.ID, now.Format(time.RFC3339Nano))
		if err != nil {
			if isUniqueViolation(err) {
				continue
			}
			return nil, err
		}
		claimed = append(claimed, step)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return claimed, nil
}

func (s *Store) CompleteStep(
	ctx context.Context,
	step domain.PipelineStep,
	outcome domain.State,
	result json.RawMessage,
	message string,
) error {
	effectOutcome := outcome
	stateOutcome := outcome
	if outcome == domain.StateFailed && step.Attempts < step.MaxAttempts {
		stateOutcome = domain.StatePending
	}
	if err := domain.ValidateTransition(domain.StateRunning, stateOutcome); err != nil {
		return err
	}
	step.State, step.Result, step.Error = stateOutcome, result, message
	now := nowUTC()
	step.UpdatedAt = now
	if stateOutcome != domain.StatePending {
		step.EndedAt = &now
	}
	data, err := marshal(step)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	resultSQL, err := tx.ExecContext(ctx, `
UPDATE pipeline_steps SET state=?,payload=?,updated_at=? WHERE id=? AND state='running'`,
		stateOutcome, data, now.Format(time.RFC3339Nano), step.ID)
	if err != nil {
		return err
	}
	if n, _ := resultSQL.RowsAffected(); n != 1 {
		return fmt.Errorf("pipeline step %q is no longer running", step.ID)
	}
	_, err = tx.ExecContext(ctx, `
UPDATE effect_receipts SET state=?,result=?,completed_at=?
WHERE target_type='pipeline_step' AND target_id=? AND state='running'`,
		effectOutcome, result, now.Format(time.RFC3339Nano), step.ID)
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return s.refreshPipelineState(ctx, step.PipelineID)
}

func (s *Store) ResolveRecognizedWork(
	ctx context.Context,
	workID string,
	outcome domain.State,
	ruleID string,
	result json.RawMessage,
	message string,
) error {
	var targetType string
	err := s.db.QueryRowContext(ctx,
		`SELECT target_type FROM effect_receipts WHERE target_id=?`, workID).Scan(&targetType)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	switch targetType {
	case "queue_item":
		return s.CompleteQueue(ctx, workID, outcome, ruleID, result, message)
	case "pipeline_step":
		return s.resolveWaitingStep(ctx, workID, outcome, result, message)
	default:
		return fmt.Errorf("unsupported recognized effect target %q", targetType)
	}
}

func (s *Store) resolveWaitingStep(
	ctx context.Context,
	stepID string,
	outcome domain.State,
	result json.RawMessage,
	message string,
) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var data []byte
	var current domain.State
	err = tx.QueryRowContext(ctx,
		`SELECT payload,state FROM pipeline_steps WHERE id=?`, stepID).Scan(&data, &current)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	step, err := decode[domain.PipelineStep](data)
	if err != nil {
		return err
	}
	now := nowUTC()
	effectOutcome := outcome
	if outcome == domain.StateFailed && step.Attempts < step.MaxAttempts {
		outcome = domain.StatePending
	}
	if err := domain.ValidateTransition(current, outcome); err != nil {
		return err
	}
	step.State, step.Result, step.Error, step.UpdatedAt, step.EndedAt =
		outcome, result, message, now, &now
	if outcome == domain.StatePending {
		step.EndedAt = nil
	}
	updated, err := marshal(step)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
UPDATE pipeline_steps SET state=?,payload=?,updated_at=? WHERE id=?`,
		outcome, updated, now.Format(time.RFC3339Nano), stepID)
	if err == nil {
		_, err = tx.ExecContext(ctx, `
UPDATE effect_receipts SET state=?,result=?,completed_at=?
WHERE target_type='pipeline_step' AND target_id=?`,
			effectOutcome, result, now.Format(time.RFC3339Nano), stepID)
	}
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return s.refreshPipelineState(ctx, step.PipelineID)
}

func (s *Store) refreshPipelineState(ctx context.Context, pipelineID string) error {
	rows, err := s.db.QueryContext(ctx,
		`SELECT state FROM pipeline_steps WHERE pipeline_id=?`, pipelineID)
	if err != nil {
		return err
	}
	defer rows.Close()
	allSucceeded := true
	anyFailed := false
	anyActive := false
	count := 0
	for rows.Next() {
		count++
		var state domain.State
		if err := rows.Scan(&state); err != nil {
			return err
		}
		if state != domain.StateSucceeded {
			allSucceeded = false
		}
		if state == domain.StateFailed || state == domain.StateError {
			anyFailed = true
		}
		if state == domain.StatePending || state == domain.StateRunning ||
			state == domain.StateWaiting || state == domain.StatePaused ||
			state == domain.StateInterrupted {
			anyActive = true
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if count == 0 || anyActive {
		return nil
	}
	state := domain.StateSucceeded
	if anyFailed || !allSucceeded {
		state = domain.StateFailed
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE pipelines SET state=?,updated_at=? WHERE id=?`,
		state, nowUTC().Format(time.RFC3339Nano), pipelineID)
	if err != nil {
		return err
	}
	return s.generatePipelineReport(ctx, pipelineID, state)
}

func (s *Store) generatePipelineReport(ctx context.Context, pipelineID string, state domain.State) error {
	var pipelineData []byte
	if err := s.db.QueryRowContext(ctx,
		`SELECT payload FROM pipelines WHERE id=?`, pipelineID).Scan(&pipelineData); err != nil {
		return err
	}
	pipeline, err := decode[domain.Pipeline](pipelineData)
	if err != nil {
		return err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT payload,state FROM pipeline_steps WHERE pipeline_id=? ORDER BY created_at`, pipelineID)
	if err != nil {
		return err
	}
	var steps []domain.PipelineStep
	for rows.Next() {
		var data []byte
		var stepState domain.State
		if err := rows.Scan(&data, &stepState); err != nil {
			rows.Close()
			return err
		}
		step, err := decode[domain.PipelineStep](data)
		if err != nil {
			rows.Close()
			return err
		}
		step.State = stepState
		steps = append(steps, step)
	}
	rows.Close()
	metric, err := s.AggregatePipelineMetrics(ctx, pipelineID)
	if err != nil {
		return err
	}
	_, err = s.SaveReport(ctx, pipeline.ProjectID, "pipeline", pipelineID, map[string]any{
		"request": pipeline.Request, "pipeline": pipeline.Name, "state": state,
		"steps": steps, "metrics": metric, "verdict": state,
	})
	return err
}

func (s *Store) AcquireWorkspaceLock(
	ctx context.Context,
	workspace, ownerID, mode string,
	files []string,
) error {
	if mode != "read" && mode != "write" {
		return fmt.Errorf("invalid workspace lock mode %q", mode)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx,
		`SELECT owner_id,mode,files FROM workspace_locks WHERE workspace=?`, workspace)
	if err != nil {
		return err
	}
	for rows.Next() {
		var existingOwner, existingMode string
		var data []byte
		if err := rows.Scan(&existingOwner, &existingMode, &data); err != nil {
			rows.Close()
			return err
		}
		if existingOwner == ownerID {
			continue
		}
		var existingFiles []string
		_ = json.Unmarshal(data, &existingFiles)
		if mode == "write" || existingMode == "write" {
			if fileSetsOverlap(files, existingFiles) {
				rows.Close()
				return fmt.Errorf("workspace conflict with %s lock owned by %s", existingMode, existingOwner)
			}
		}
	}
	rows.Close()
	data, _ := json.Marshal(files)
	_, err = tx.ExecContext(ctx, `
INSERT INTO workspace_locks(workspace,owner_id,mode,files,acquired_at)
VALUES(?,?,?,?,?)
ON CONFLICT(workspace,owner_id) DO UPDATE SET mode=excluded.mode,files=excluded.files,acquired_at=excluded.acquired_at`,
		workspace, ownerID, mode, data, nowUTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	return tx.Commit()
}

func fileSetsOverlap(a, b []string) bool {
	if len(a) == 0 || len(b) == 0 {
		return true
	}
	set := make(map[string]bool, len(a))
	for _, file := range a {
		set[file] = true
	}
	for _, file := range b {
		if set[file] {
			return true
		}
	}
	return false
}

func (s *Store) ReleaseWorkspaceLock(ctx context.Context, workspace, ownerID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM workspace_locks WHERE workspace=? AND owner_id=?`, workspace, ownerID)
	return err
}

func (s *Store) AggregateMetrics(ctx context.Context, projectID string) (domain.Metric, error) {
	var out domain.Metric
	query := `
SELECT COALESCE(SUM(input_tokens),0),COALESCE(SUM(output_tokens),0),
 COALESCE(SUM(cache_read),0),COALESCE(SUM(cache_write),0),
 COALESCE(SUM(duration_ms),0),COALESCE(SUM(cost_micros_usd),0)
FROM metrics`
	var row *sql.Row
	if projectID == "" {
		row = s.db.QueryRowContext(ctx, query)
	} else {
		row = s.db.QueryRowContext(ctx, query+` WHERE project_id=?`, projectID)
	}
	err := row.Scan(&out.InputTokens, &out.OutputTokens, &out.CacheRead,
		&out.CacheWrite, &out.Duration, &out.CostMicrosUSD)
	return out, err
}

func (s *Store) AggregatePipelineMetrics(ctx context.Context, pipelineID string) (domain.Metric, error) {
	var out domain.Metric
	err := s.db.QueryRowContext(ctx, `
SELECT COALESCE(SUM(input_tokens),0),COALESCE(SUM(output_tokens),0),
 COALESCE(SUM(cache_read),0),COALESCE(SUM(cache_write),0),
 COALESCE(SUM(duration_ms),0),COALESCE(SUM(cost_micros_usd),0)
FROM metrics
WHERE pipeline_step_id IN (SELECT id FROM pipeline_steps WHERE pipeline_id=?)`,
		pipelineID).Scan(&out.InputTokens, &out.OutputTokens, &out.CacheRead,
		&out.CacheWrite, &out.Duration, &out.CostMicrosUSD)
	return out, err
}

func (s *Store) SetPipelineState(ctx context.Context, pipelineID string, state domain.State, message string) error {
	var data []byte
	var current domain.State
	err := s.db.QueryRowContext(ctx,
		`SELECT payload,state FROM pipelines WHERE id=?`, pipelineID).Scan(&data, &current)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if current != state {
		if err := domain.ValidateTransition(current, state); err != nil {
			return err
		}
	}
	pipeline, err := decode[domain.Pipeline](data)
	if err != nil {
		return err
	}
	pipeline.State, pipeline.Error, pipeline.UpdatedAt = state, message, nowUTC()
	updated, err := marshal(pipeline)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE pipelines SET state=?,payload=?,updated_at=? WHERE id=?`,
		state, updated, pipeline.UpdatedAt.Format(time.RFC3339Nano), pipelineID)
	return err
}

func (s *Store) ListLogs(ctx context.Context, projectID string, limit int) ([]domain.LogEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	query := `SELECT id,COALESCE(project_id,''),level,kind,message,fields,created_at FROM logs`
	args := []any{}
	if projectID != "" {
		query += ` WHERE project_id=?`
		args = append(args, projectID)
	}
	query += ` ORDER BY sequence DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.LogEntry
	for rows.Next() {
		var v domain.LogEntry
		var fields []byte
		var created string
		if err := rows.Scan(&v.ID, &v.ProjectID, &v.Level, &v.Kind,
			&v.Message, &fields, &created); err != nil {
			return nil, err
		}
		v.Fields = fields
		v.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, rows.Err()
}
