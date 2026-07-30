package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jgcastro09/sessionhub/internal/domain"
	"github.com/jgcastro09/sessionhub/internal/id"
	"github.com/jgcastro09/sessionhub/internal/metrics"
	"github.com/jgcastro09/sessionhub/internal/store"
	"github.com/jgcastro09/sessionhub/internal/terminal"
)

type Service struct {
	ctx        context.Context
	store      *store.Store
	terminals  *terminal.Manager
	mu         sync.RWMutex
	instances  map[string]domain.Instance
	configs    map[string]domain.ExecutorConfig
	work       map[string]*activeWork
	calculator *metrics.Calculator
}

type activeWork struct {
	ID         string
	InstanceID string
	Prompt     string
	Output     []byte
	StartedAt  time.Time
	LastOutput time.Time
	Deadline   time.Time
	// done is set by the simple Automation scheduler. Queue and pipeline
	// callers continue using the persisted-work resolution path below.
	done chan WorkResult
}

// WorkResult is the bounded completion evidence returned to the simple
// Automation scheduler. It deliberately exposes a preview rather than a
// full terminal transcript, which may contain a large or sensitive output.
type WorkResult struct {
	InstanceID string
	Outcome    domain.State
	Reason     string
	Output     string
}

// AutomationProgress receives a bounded live terminal snapshot after the
// automation has successfully acquired the PTY and sent its prompt.
// Callers use it for status only; recognition and process ownership remain
// entirely inside Service.
type AutomationProgress func(instanceID, output string)

func New(ctx context.Context, repository *store.Store, terminals *terminal.Manager) *Service {
	return &Service{
		ctx: ctx, store: repository, terminals: terminals,
		instances:  make(map[string]domain.Instance),
		configs:    make(map[string]domain.ExecutorConfig),
		work:       make(map[string]*activeWork),
		calculator: metrics.NewCalculator(),
	}
}

func (s *Service) Run() {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case event := <-s.terminals.Events():
			s.handleEvent(event)
		case now := <-ticker.C:
			s.checkWork(now)
		}
	}
}

func (s *Service) handleEvent(event terminal.Event) {
	s.mu.Lock()
	instance, ok := s.instances[event.InstanceID]
	if !ok {
		s.mu.Unlock()
		return
	}
	if event.Kind == terminal.EventState {
		instance.State = event.State
		now := time.Now().UTC()
		if event.State == domain.StateRunning && instance.StartedAt == nil {
			instance.StartedAt = &now
		}
		if event.State.Terminal() || event.State == domain.StateInterrupted ||
			event.State == domain.StateError || event.State == domain.StateFailed {
			instance.EndedAt = &now
		}
		if event.Err != nil {
			instance.Error = event.Err.Error()
		}
		instance.ExitCode = event.ExitCode
		s.instances[event.InstanceID] = instance
	}
	// Capture the exit recognition while the lock is held, but defer the
	// calls that take s.mu themselves (handleOutput, finishWork) until after
	// it is released — sync.RWMutex isn't reentrant, so calling them here
	// would deadlock this goroutine forever, permanently freezing
	// Service.Run()'s event loop while still holding the lock Start() needs
	// to register any future instance.
	var exitRecognition Recognition
	if event.Kind == terminal.EventState && event.ExitCode != nil {
		if _, hasWork := s.work[event.InstanceID]; hasWork {
			config := s.configs[event.InstanceID]
			recognition := RecognizeExit(config.Rules, *event.ExitCode)
			if recognition.Matched {
				exitRecognition = recognition
			} else if *event.ExitCode == 0 {
				exitRecognition = Recognition{Matched: true, RuleID: "process_exit", Outcome: domain.StateSucceeded, Reason: "process exited successfully"}
			} else {
				exitRecognition = Recognition{Matched: true, RuleID: "process_exit", Outcome: domain.StateFailed, Reason: fmt.Sprintf("process exited with status %d", *event.ExitCode)}
			}
		}
	}
	s.mu.Unlock()
	if event.Kind == terminal.EventOutput && len(event.Data) > 0 {
		s.handleOutput(event.InstanceID, event.Data)
	}
	if exitRecognition.Matched {
		s.finishWork(event.InstanceID, exitRecognition)
	}
	if event.Kind == terminal.EventState {
		_ = s.store.UpdateInstance(context.Background(), instance)
	}
	if event.Kind == terminal.EventError && event.Err != nil {
		_ = s.store.Log(context.Background(), domain.LogEntry{
			SessionID: instance.SessionID, Level: "error", Kind: "terminal",
			Message: event.Err.Error(),
		})
	}
}

// StartOrReuse uses the normal Session Hub instance lifecycle. An existing
// live terminal for the same session/executor is retained; otherwise Start
// creates it with the session workspace and normal PTY manager.
func (s *Service) StartOrReuse(ctx context.Context, sessionID, executorID string, width, height int) (*terminal.Session, domain.Instance, error) {
	s.mu.RLock()
	for _, instance := range s.instances {
		if instance.SessionID == sessionID && instance.ExecutorID == executorID &&
			(instance.State == domain.StateRunning || instance.State == domain.StateWaiting) {
			if live, ok := s.terminals.Get(instance.ID); ok && live.State() == domain.StateRunning {
				s.mu.RUnlock()
				return live, instance, nil
			}
		}
	}
	s.mu.RUnlock()
	return s.Start(ctx, sessionID, executorID, width, height)
}

// RunAutomationStep is the sequential, in-process automation bridge. It
// intentionally delegates process creation, PTY writes, terminal leases and
// recognition to Service rather than reimplementing any of those concerns.
func (s *Service) RunAutomationStep(ctx context.Context, workID, sessionID, executorID, prompt string, width, height int) (WorkResult, error) {
	return s.runAutomationStep(ctx, workID, sessionID, executorID, prompt, width, height, nil)
}

// RunAutomationStepWithProgress is RunAutomationStep plus best-effort live
// PTY feedback for the Automation UI. It does not expose a second terminal
// control path or change recognition semantics.
func (s *Service) RunAutomationStepWithProgress(ctx context.Context, workID, sessionID, executorID, prompt string, width, height int, progress AutomationProgress) (WorkResult, error) {
	return s.runAutomationStep(ctx, workID, sessionID, executorID, prompt, width, height, progress)
}

func (s *Service) runAutomationStep(ctx context.Context, workID, sessionID, executorID, prompt string, width, height int, progress AutomationProgress) (WorkResult, error) {
	term, instance, err := s.StartOrReuse(ctx, sessionID, executorID, width, height)
	if err != nil {
		return WorkResult{}, err
	}
	owner := terminal.Owner{Kind: "automation", ID: workID}
	// An automation deliberately works in the session's already-open CLI
	// tab. Hub mode can leave that tab leased to the local operator even
	// though the person is looking at Automation, so hand that specific
	// local lease to the automation rather than retrying forever. Remote and
	// other automation leases remain protected by Acquire below.
	localOwner := terminal.Owner{Kind: "local", ID: "operator"}
	tookLocalLease := false
	if term.Owner().Equal(localOwner) {
		_ = term.Release(localOwner)
		tookLocalLease = true
	}
	if err := term.Acquire(owner); err != nil {
		return WorkResult{InstanceID: instance.ID}, err
	}
	defer func() {
		_ = term.Release(owner)
		if tookLocalLease {
			_ = term.Acquire(localOwner)
		}
	}()
	if err := term.SendPrompt(owner, prompt); err != nil {
		return WorkResult{InstanceID: instance.ID}, err
	}
	now := time.Now().UTC()
	done := make(chan WorkResult, 1)
	s.mu.Lock()
	config := s.configs[instance.ID]
	work := &activeWork{ID: workID, InstanceID: instance.ID, Prompt: prompt, StartedAt: now, LastOutput: now, done: done}
	if config.Timeout > 0 {
		work.Deadline = now.Add(config.Timeout)
	}
	s.work[instance.ID] = work
	s.mu.Unlock()
	if progress != nil {
		progress(instance.ID, "")
	}
	updates := time.NewTicker(time.Second)
	defer updates.Stop()

	for {
		select {
		case result := <-done:
			if result.Outcome == domain.StateSucceeded || result.Outcome == domain.StateFinished {
				return result, nil
			}
			return result, fmt.Errorf("%s", result.Reason)
		case <-ctx.Done():
			s.mu.Lock()
			if current := s.work[instance.ID]; current != nil && current.ID == workID {
				delete(s.work, instance.ID)
			}
			s.mu.Unlock()
			// Stop is the established, process-tree-safe cancellation path. It
			// is also used when an automation is cancelled or Session Hub closes.
			_ = s.Stop(context.Background(), instance.ID)
			return WorkResult{InstanceID: instance.ID, Outcome: domain.StateCanceled, Reason: ctx.Err().Error()}, ctx.Err()
		case <-updates.C:
			if progress != nil {
				progress(instance.ID, outputPreview([]byte(term.Snapshot())))
			}
		}
	}
}

func (s *Service) Start(
	ctx context.Context,
	sessionID, executorID string,
	width, height int,
) (*terminal.Session, domain.Instance, error) {
	cfg, err := s.store.GetExecutor(ctx, executorID)
	if err != nil {
		return nil, domain.Instance{}, err
	}
	if sessionID != "" {
		if sess, err := s.store.GetSession(ctx, sessionID); err == nil && strings.TrimSpace(sess.Workspace) != "" {
			cfg.WorkingDir = strings.TrimSpace(sess.Workspace)
		}
	}
	instance, err := s.store.CreateInstance(ctx, domain.Instance{
		ID: id.New("inst"), SessionID: sessionID, ExecutorID: executorID,
		State: domain.StatePending,
	})
	if err != nil {
		return nil, domain.Instance{}, err
	}
	session, err := s.terminals.Start(instance.ID, cfg, width, height)
	if err != nil {
		instance.State, instance.Error = domain.StateFailed, err.Error()
		_ = s.store.UpdateInstance(ctx, instance)
		return nil, instance, err
	}
	now := time.Now().UTC()
	instance.State, instance.StartedAt = domain.StateRunning, &now
	if err := s.store.UpdateInstance(ctx, instance); err != nil {
		_ = s.terminals.Stop(instance.ID, time.Second)
		return nil, instance, fmt.Errorf("persist started instance: %w", err)
	}
	s.mu.Lock()
	s.instances[instance.ID] = instance
	s.configs[instance.ID] = cfg
	s.mu.Unlock()
	return session, instance, nil
}

func (s *Service) Stop(ctx context.Context, instanceID string) error {
	if err := s.terminals.Stop(instanceID, 2*time.Second); err != nil {
		return err
	}
	s.mu.Lock()
	instance, ok := s.instances[instanceID]
	if ok {
		now := time.Now().UTC()
		instance.State, instance.EndedAt = domain.StateInterrupted, &now
		s.instances[instanceID] = instance
	}
	s.mu.Unlock()
	if ok {
		return s.store.UpdateInstance(ctx, instance)
	}
	return nil
}

func (s *Service) Active(instanceID string) (*terminal.Session, bool) {
	return s.terminals.Get(instanceID)
}

// DispatchPrompt is the provider-neutral automation path. It writes through
// the same PTY as the operator and respects the terminal's explicit lease.
func (s *Service) DispatchPrompt(
	ctx context.Context,
	workID, sessionID, executorID, prompt string,
) error {
	s.mu.RLock()
	var selected domain.Instance
	for _, instance := range s.instances {
		if instance.SessionID == sessionID && instance.ExecutorID == executorID &&
			(instance.State == domain.StateRunning || instance.State == domain.StateWaiting) {
			selected = instance
			break
		}
	}
	s.mu.RUnlock()
	if selected.ID == "" {
		return fmt.Errorf("no active terminal for executor %q in session %q", executorID, sessionID)
	}
	term, ok := s.terminals.Get(selected.ID)
	if !ok {
		return fmt.Errorf("terminal %q is no longer active", selected.ID)
	}
	owner := terminal.Owner{Kind: "automation", ID: workID}
	if err := term.Acquire(owner); err != nil {
		return err
	}
	defer term.Release(owner)
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		if err := term.SendPrompt(owner, prompt); err != nil {
			return err
		}
		now := time.Now().UTC()
		s.mu.Lock()
		config := s.configs[selected.ID]
		work := &activeWork{
			ID: workID, InstanceID: selected.ID, Prompt: prompt,
			StartedAt: now, LastOutput: now,
		}
		if config.Timeout > 0 {
			work.Deadline = now.Add(config.Timeout)
		}
		s.work[selected.ID] = work
		s.mu.Unlock()
		return nil
	}
}

func (s *Service) handleOutput(instanceID string, data []byte) {
	s.mu.Lock()
	work := s.work[instanceID]
	config := s.configs[instanceID]
	if work == nil {
		s.mu.Unlock()
		return
	}
	work.Output = append(work.Output, data...)
	work.LastOutput = time.Now().UTC()
	if len(work.Output) > 4<<20 {
		work.Output = append([]byte(nil), work.Output[len(work.Output)-(4<<20):]...)
	}
	recognition := RecognizeOutput(config.Rules, string(work.Output))
	s.mu.Unlock()
	if recognition.Matched {
		s.finishWork(instanceID, recognition)
	}
}

func (s *Service) checkWork(now time.Time) {
	s.mu.RLock()
	var checks []struct {
		instanceID string
		work       activeWork
		config     domain.ExecutorConfig
	}
	for instanceID, work := range s.work {
		checks = append(checks, struct {
			instanceID string
			work       activeWork
			config     domain.ExecutorConfig
		}{instanceID, *work, s.configs[instanceID]})
	}
	s.mu.RUnlock()
	for _, check := range checks {
		if !check.work.Deadline.IsZero() && !now.Before(check.work.Deadline) {
			s.finishWork(check.instanceID, Recognition{
				Matched: true, RuleID: "timeout", Outcome: domain.StateFailed,
				Reason: "configured Executor timeout reached",
			})
			continue
		}
		recognition := RecognizeStable(check.config.Rules, now.Sub(check.work.LastOutput))
		if recognition.Matched {
			s.finishWork(check.instanceID, recognition)
		}
	}
}

func (s *Service) finishWork(instanceID string, recognition Recognition) {
	s.mu.Lock()
	work := s.work[instanceID]
	config := s.configs[instanceID]
	instance := s.instances[instanceID]
	if work == nil {
		s.mu.Unlock()
		return
	}
	delete(s.work, instanceID)
	s.mu.Unlock()

	outcome := recognition.Outcome
	if recognition.Ambiguous {
		outcome = domain.StateWaiting
	}
	result, _ := json.Marshal(map[string]any{
		"recognition_rule": recognition.RuleID,
		"reason":           recognition.Reason,
		"output":           string(work.Output),
	})
	if work.done != nil {
		work.done <- WorkResult{InstanceID: instanceID, Outcome: outcome, Reason: recognition.Reason, Output: outputPreview(work.Output)}
	} else {
		err := s.store.ResolveRecognizedWork(context.Background(), work.ID, outcome,
			recognition.RuleID, result, recognition.Reason)
		if err != nil {
			_ = s.store.Log(context.Background(), domain.LogEntry{
				SessionID: instance.SessionID, Level: "error", Kind: "recognition",
				Message: err.Error(),
			})
		}
	}
	metric := s.calculator.Measure(work.Prompt, string(work.Output), config.Tokenizer, metrics.Usage{})
	metric.SessionID, metric.ExecutorID, metric.InstanceID =
		instance.SessionID, instance.ExecutorID, instanceID
	metric.Duration = time.Since(work.StartedAt).Milliseconds()
	metric.PromptCount, metric.ResponseCount = 1, 1
	if config.PriceID != "" {
		if price, err := s.store.GetPrice(context.Background(), config.PriceID); err == nil {
			metric = metrics.ApplyPrice(metric, price)
		}
	}
	_ = s.store.SaveMetric(context.Background(), metric)
}

func outputPreview(output []byte) string {
	const max = 1200
	if len(output) <= max {
		return string(output)
	}
	return string(output[len(output)-max:])
}
