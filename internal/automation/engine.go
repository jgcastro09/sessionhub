package automation

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/jgcastro09/sessionhub/internal/domain"
	"github.com/jgcastro09/sessionhub/internal/store"
)

type PromptDispatcher interface {
	DispatchPrompt(context.Context, string, string, string, string) error
}

type Engine struct {
	store       *store.Store
	dispatcher  PromptDispatcher
	concurrency int
	sem         chan struct{}
	wg          sync.WaitGroup
}

func NewEngine(repository *store.Store, dispatcher PromptDispatcher, concurrency int) *Engine {
	if concurrency < 1 {
		concurrency = 1
	}
	return &Engine{
		store: repository, dispatcher: dispatcher, concurrency: concurrency,
		sem: make(chan struct{}, concurrency),
	}
}

func (e *Engine) RunQueueOnce(ctx context.Context, sessionID string) error {
	item, err := e.store.ClaimNextQueue(ctx, sessionID)
	if err != nil || item == nil {
		return err
	}
	if item.NeedsApproval {
		return e.store.CompleteQueue(ctx, item.ID, domain.StateWaiting, "approval", nil,
			"manual approval required before PTY input")
	}
	if err := CheckBudget(item.Budget, Consumption{Attempts: item.Attempts - 1}); err != nil {
		return e.store.CompleteQueue(ctx, item.ID, domain.StateFailed, "budget", nil, err.Error())
	}
	if e.dispatcher == nil {
		return e.store.CompleteQueue(ctx, item.ID, domain.StateFailed, "dispatcher", nil,
			"no prompt dispatcher is active")
	}
	err = e.dispatcher.DispatchPrompt(ctx, item.ID, item.SessionID, item.ExecutorID, item.Prompt)
	if err != nil {
		return e.store.CompleteQueue(ctx, item.ID, domain.StateWaiting, "control", nil,
			"PTY input was not sent: "+err.Error())
	}
	// A successful write is not successful work. The configured recognition
	// rule or a manual confirmation must complete the item.
	return e.store.CompleteQueue(ctx, item.ID, domain.StateWaiting, "pty_write", nil,
		"prompt sent; waiting for configured completion evidence")
}

func (e *Engine) RunPipelineReady(
	ctx context.Context,
	pipeline domain.Pipeline,
	workspace string,
) (int, error) {
	limit := e.concurrency
	if pipeline.Budget.Concurrency > 0 && pipeline.Budget.Concurrency < limit {
		limit = pipeline.Budget.Concurrency
	}
	usage, err := e.store.AggregatePipelineMetrics(ctx, pipeline.ID)
	if err != nil {
		return 0, err
	}
	duration := time.Duration(0)
	if pipeline.StartedAt != nil {
		duration = time.Since(*pipeline.StartedAt)
	}
	if err := CheckBudget(pipeline.Budget, Consumption{
		InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens,
		CostMicrosUSD: usage.CostMicrosUSD, Duration: duration,
	}); err != nil {
		_ = e.store.SetPipelineState(ctx, pipeline.ID, domain.StatePaused, err.Error())
		return 0, err
	}
	steps, err := e.store.ClaimReadySteps(ctx, pipeline.ID, limit)
	if err != nil {
		return 0, err
	}
	for _, step := range steps {
		step := step
		e.sem <- struct{}{}
		e.wg.Add(1)
		go func() {
			defer func() { <-e.sem; e.wg.Done() }()
			e.executeStep(ctx, step, workspace)
		}()
	}
	return len(steps), nil
}

func (e *Engine) executeStep(ctx context.Context, step domain.PipelineStep, workspace string) {
	mode := "write"
	if step.ReadOnly {
		mode = "read"
	}
	if err := e.store.AcquireWorkspaceLock(ctx, workspace, step.ID, mode, step.Files); err != nil {
		_ = e.store.CompleteStep(ctx, step, domain.StatePaused, nil, err.Error())
		return
	}
	defer e.store.ReleaseWorkspaceLock(context.Background(), workspace, step.ID)

	var result any
	var outcome = domain.StateSucceeded
	var message string
	switch step.Type {
	case domain.StepCommand:
		var config CommandConfig
		if err := json.Unmarshal(step.Config, &config); err != nil {
			outcome, message = domain.StateFailed, "decode deterministic command: "+err.Error()
			break
		}
		if config.Sensitive {
			outcome, message = domain.StateWaiting, "manual approval required for sensitive command"
			break
		}
		commandResult, err := RunCommand(ctx, config)
		result = commandResult
		if err != nil {
			outcome, message = domain.StateFailed, err.Error()
		}
	case domain.StepPrompt:
		var config struct {
			SessionID  string `json:"session_id"`
			ExecutorID string `json:"executor_id"`
			Prompt     string `json:"prompt"`
		}
		if err := json.Unmarshal(step.Config, &config); err != nil {
			outcome, message = domain.StateFailed, "decode prompt step: "+err.Error()
			break
		}
		if e.dispatcher == nil {
			outcome, message = domain.StateFailed, "no prompt dispatcher is active"
			break
		}
		if err := e.dispatcher.DispatchPrompt(ctx, step.ID, config.SessionID, config.ExecutorID, config.Prompt); err != nil {
			outcome, message = domain.StateWaiting, "PTY input was not sent: "+err.Error()
		} else {
			outcome, message = domain.StateWaiting, "prompt sent; awaiting completion evidence"
		}
	case domain.StepApproval:
		outcome, message = domain.StateWaiting, "manual approval required"
		var config struct {
			SessionID string `json:"session_id"`
			Reason    string `json:"reason"`
		}
		_ = json.Unmarshal(step.Config, &config)
		if config.Reason == "" {
			config.Reason = step.Name
		}
		if _, err := e.store.CreateApproval(ctx, domain.Approval{
			SessionID: config.SessionID, TargetType: "pipeline_step",
			TargetID: step.ID, Reason: config.Reason,
		}); err != nil {
			outcome, message = domain.StateFailed, "create approval: "+err.Error()
		}
	case domain.StepCondition:
		var config struct {
			Value bool `json:"value"`
		}
		if err := json.Unmarshal(step.Config, &config); err != nil {
			outcome, message = domain.StateFailed, "decode condition: "+err.Error()
		} else if !config.Value {
			outcome, message = domain.StateCanceled, "condition evaluated false"
		}
	case domain.StepParallel:
		result = map[string]string{"status": "parallel dependencies released"}
	case domain.StepConsolidate:
		result = map[string]string{"status": "dependency results ready for configured consolidation"}
	default:
		outcome, message = domain.StateFailed, fmt.Sprintf("unknown pipeline step type %q", step.Type)
	}
	data, _ := json.Marshal(result)
	_ = e.store.CompleteStep(context.Background(), step, outcome, data, message)
}

func (e *Engine) Wait() { e.wg.Wait() }

func (e *Engine) ProcessSchedules(ctx context.Context, at time.Time) error {
	schedules, err := e.store.DueSchedules(ctx, at)
	if err != nil {
		return err
	}
	for _, schedule := range schedules {
		switch schedule.TargetType {
		case "prompt":
			var target struct {
				ExecutorID string `json:"executor_id"`
				Prompt     string `json:"prompt"`
			}
			if err := json.Unmarshal(schedule.Target, &target); err != nil {
				return fmt.Errorf("decode schedule %q target: %w", schedule.Name, err)
			}
			_, err = e.store.Enqueue(ctx, domain.QueueItem{
				SessionID: schedule.SessionID, ExecutorID: target.ExecutorID,
				Prompt: target.Prompt, MaxAttempts: 1,
				IdempotencyKey: fmt.Sprintf("schedule:%s:%s", schedule.ID, schedule.NextRun.UTC().Format(time.RFC3339Nano)),
			})
			if err != nil {
				return err
			}
		default:
			return fmt.Errorf("schedule %q has unsupported target type %q", schedule.Name, schedule.TargetType)
		}
		now := at.UTC()
		schedule.LastRun = &now
		next, err := NextOccurrence(schedule, at)
		if err != nil {
			return err
		}
		if next.IsZero() {
			schedule.Enabled, schedule.NextRun = false, nil
		} else {
			schedule.NextRun = &next
		}
		if _, err := e.store.SaveSchedule(ctx, schedule); err != nil {
			return err
		}
	}
	return nil
}
