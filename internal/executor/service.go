package executor

import (
	"context"
	"encoding/json"
	"fmt"
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
}

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
	if event.Kind == terminal.EventOutput && len(event.Data) > 0 {
		s.handleOutput(event.InstanceID, event.Data)
	}
	if event.Kind == terminal.EventState && event.ExitCode != nil {
		// s.mu.Lock() (a write lock) is already held from the top of this
		// function — sync.RWMutex isn't reentrant, so re-acquiring it here
		// (even as RLock) would deadlock this goroutine forever, permanently
		// freezing Service.Run()'s event loop while still holding the lock
		// Start() needs to register any future instance.
		_, hasWork := s.work[event.InstanceID]
		config := s.configs[event.InstanceID]
		if hasWork {
			recognition := RecognizeExit(config.Rules, *event.ExitCode)
			if recognition.Matched {
				s.finishWork(event.InstanceID, recognition)
			}
		}
	}
	s.mu.Unlock()
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

func (s *Service) Start(
	ctx context.Context,
	sessionID, executorID string,
	width, height int,
) (*terminal.Session, domain.Instance, error) {
	cfg, err := s.store.GetExecutor(ctx, executorID)
	if err != nil {
		return nil, domain.Instance{}, err
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
	err := s.store.ResolveRecognizedWork(context.Background(), work.ID, outcome,
		recognition.RuleID, result, recognition.Reason)
	if err != nil {
		_ = s.store.Log(context.Background(), domain.LogEntry{
			SessionID: instance.SessionID, Level: "error", Kind: "recognition",
			Message: err.Error(),
		})
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
