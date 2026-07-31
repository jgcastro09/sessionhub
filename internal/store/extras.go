package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jgcastro09/sessionhub/internal/domain"
	"github.com/jgcastro09/sessionhub/internal/id"
)

func (s *Store) SavePrice(ctx context.Context, price domain.Price) (domain.Price, error) {
	if price.ID == "" {
		price.ID = id.New("price")
	}
	if price.Model == "" || price.Version == "" {
		return price, fmt.Errorf("price model and version are required")
	}
	if price.EffectiveAt.IsZero() {
		price.EffectiveAt = nowUTC()
	}
	data, err := marshal(price)
	if err != nil {
		return price, err
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO prices(id,model,version,payload,effective_at) VALUES(?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET model=excluded.model,version=excluded.version,
 payload=excluded.payload,effective_at=excluded.effective_at`,
		price.ID, price.Model, price.Version, data, price.EffectiveAt.Format(time.RFC3339Nano))
	if err != nil {
		return price, fmt.Errorf("save price: %w", err)
	}
	return price, nil
}

func (s *Store) ListPrices(ctx context.Context) ([]domain.Price, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT payload FROM prices ORDER BY model, effective_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Price
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		value, err := decode[domain.Price](data)
		if err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, rows.Err()
}

func (s *Store) GetPrice(ctx context.Context, priceID string) (domain.Price, error) {
	var data []byte
	err := s.db.QueryRowContext(ctx, `SELECT payload FROM prices WHERE id=?`, priceID).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Price{}, ErrNotFound
	}
	if err != nil {
		return domain.Price{}, err
	}
	return decode[domain.Price](data)
}

func (s *Store) CreateApproval(ctx context.Context, approval domain.Approval) (domain.Approval, error) {
	if approval.ID == "" {
		approval.ID = id.New("approval")
	}
	if approval.State == "" {
		approval.State = domain.StateWaiting
	}
	if approval.RequestedAt.IsZero() {
		approval.RequestedAt = nowUTC()
	}
	data, err := marshal(approval)
	if err != nil {
		return approval, err
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO approvals(id,project_id,target_type,target_id,state,payload,requested_at)
VALUES(?,?,?,?,?,?,?)`, approval.ID, approval.ProjectID, approval.TargetType,
		approval.TargetID, approval.State, data, approval.RequestedAt.Format(time.RFC3339Nano))
	if err != nil {
		return approval, fmt.Errorf("create approval: %w", err)
	}
	return approval, nil
}

func (s *Store) DecideApproval(
	ctx context.Context,
	approvalID, actor, decision string,
) (domain.Approval, error) {
	if decision != "approved" && decision != "rejected" {
		return domain.Approval{}, fmt.Errorf("approval decision must be approved or rejected")
	}
	var data []byte
	var current domain.State
	err := s.db.QueryRowContext(ctx,
		`SELECT payload,state FROM approvals WHERE id=?`, approvalID).Scan(&data, &current)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Approval{}, ErrNotFound
	}
	if err != nil {
		return domain.Approval{}, err
	}
	if current != domain.StateWaiting {
		return domain.Approval{}, fmt.Errorf("approval %q is already decided", approvalID)
	}
	approval, err := decode[domain.Approval](data)
	if err != nil {
		return approval, err
	}
	now := nowUTC()
	approval.DecidedBy, approval.Decision, approval.DecidedAt = actor, decision, &now
	if decision == "approved" {
		approval.State = domain.StateSucceeded
	} else {
		approval.State = domain.StateCanceled
	}
	updated, err := marshal(approval)
	if err != nil {
		return approval, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return approval, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `
UPDATE approvals SET state=?,payload=?,decided_at=? WHERE id=? AND state='waiting'`,
		approval.State, updated, now.Format(time.RFC3339Nano), approvalID)
	if err != nil {
		return approval, err
	}
	pipelineID := ""
	if approval.TargetType == "pipeline_step" {
		var stepData []byte
		var stepState domain.State
		err = tx.QueryRowContext(ctx,
			`SELECT payload,state FROM pipeline_steps WHERE id=?`, approval.TargetID).
			Scan(&stepData, &stepState)
		if err != nil {
			return approval, err
		}
		outcome := domain.StateSucceeded
		if decision == "rejected" {
			outcome = domain.StateCanceled
		}
		if err := domain.ValidateTransition(stepState, outcome); err != nil {
			return approval, err
		}
		step, err := decode[domain.PipelineStep](stepData)
		if err != nil {
			return approval, err
		}
		step.State, step.UpdatedAt, step.EndedAt = outcome, now, &now
		step.Result, _ = json.Marshal(map[string]string{
			"decision": decision, "actor": actor, "approval_id": approvalID,
		})
		encoded, err := marshal(step)
		if err != nil {
			return approval, err
		}
		_, err = tx.ExecContext(ctx, `
UPDATE pipeline_steps SET state=?,payload=?,updated_at=? WHERE id=?`,
			outcome, encoded, now.Format(time.RFC3339Nano), step.ID)
		if err == nil {
			_, err = tx.ExecContext(ctx, `
UPDATE effect_receipts SET state=?,result=?,completed_at=?
WHERE target_type='pipeline_step' AND target_id=? AND state='waiting'`,
				outcome, step.Result, now.Format(time.RFC3339Nano), step.ID)
		}
		if err != nil {
			return approval, err
		}
		pipelineID = step.PipelineID
	}
	if err := tx.Commit(); err != nil {
		return approval, err
	}
	if pipelineID != "" {
		err = s.refreshPipelineState(ctx, pipelineID)
	}
	return approval, err
}

func (s *Store) ListApprovals(ctx context.Context, projectID string) ([]domain.Approval, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT payload,state FROM approvals WHERE project_id=? ORDER BY requested_at DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Approval
	for rows.Next() {
		var data []byte
		var state domain.State
		if err := rows.Scan(&data, &state); err != nil {
			return nil, err
		}
		value, err := decode[domain.Approval](data)
		if err != nil {
			return nil, err
		}
		value.State = state
		out = append(out, value)
	}
	return out, rows.Err()
}

func (s *Store) SaveWatcher(ctx context.Context, watcher domain.Watcher) (domain.Watcher, error) {
	if watcher.ID == "" {
		watcher.ID = id.New("watch")
	}
	now := nowUTC()
	if watcher.CreatedAt.IsZero() {
		watcher.CreatedAt = now
	}
	watcher.UpdatedAt = now
	data, err := marshal(watcher)
	if err != nil {
		return watcher, err
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO watchers(id,project_id,enabled,workspace,last_event_hash,payload,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET enabled=excluded.enabled,workspace=excluded.workspace,
 last_event_hash=excluded.last_event_hash,payload=excluded.payload,updated_at=excluded.updated_at`,
		watcher.ID, watcher.ProjectID, watcher.Enabled, watcher.Workspace,
		watcher.LastEventHash, data, watcher.CreatedAt.Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano))
	if err != nil {
		return watcher, err
	}
	return watcher, nil
}

func (s *Store) SaveReport(
	ctx context.Context,
	projectID, targetType, targetID string,
	report any,
) (string, error) {
	reportID := id.New("report")
	data, err := json.Marshal(report)
	if err != nil {
		return "", err
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO reports(id,project_id,target_type,target_id,payload,created_at)
VALUES(?,?,?,?,?,?)`, reportID, projectID, targetType, targetID, data,
		nowUTC().Format(time.RFC3339Nano))
	return reportID, err
}
