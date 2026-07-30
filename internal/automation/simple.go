package automation

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jgcastro09/sessionhub/internal/executor"
	"github.com/jgcastro09/sessionhub/internal/id"
	"github.com/jgcastro09/sessionhub/internal/store"
)

type SimpleScheduleType string

const (
	ScheduleOnce   SimpleScheduleType = "once"
	ScheduleDaily  SimpleScheduleType = "daily"
	ScheduleWeekly SimpleScheduleType = "weekly"
)

type SimpleStatus string

const (
	StatusIdle      SimpleStatus = "Idle"
	StatusScheduled SimpleStatus = "Scheduled"
	StatusRunning   SimpleStatus = "Running"
	StatusCompleted SimpleStatus = "Completed"
	StatusFailed    SimpleStatus = "Failed"
	StatusDisabled  SimpleStatus = "Disabled"
	StatusMissed    SimpleStatus = "Missed"
	StatusCanceled  SimpleStatus = "Canceled"
)

type SimpleSchedule struct {
	Type       SimpleScheduleType `json:"type"`
	Date       string             `json:"date,omitempty"`
	Time       string             `json:"time"`
	DaysOfWeek []time.Weekday     `json:"daysOfWeek,omitempty"`
}

type SimpleStep struct {
	ID         string `json:"id"`
	ExecutorID string `json:"executorId"`
	Prompt     string `json:"prompt"`
}

type LastRun struct {
	StartedAt     *time.Time   `json:"startedAt,omitempty"`
	FinishedAt    *time.Time   `json:"finishedAt,omitempty"`
	Status        SimpleStatus `json:"status,omitempty"`
	Trigger       string       `json:"trigger,omitempty"`
	RetryCount    int          `json:"retryCount,omitempty"`
	FailedStepID  string       `json:"failedStepId,omitempty"`
	Error         string       `json:"error,omitempty"`
	OutputPreview []string     `json:"outputPreview,omitempty"`
}

const automationRetryDelay = 10 * time.Second

type SimpleAutomation struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	SessionID   string         `json:"sessionId"`
	Enabled     bool           `json:"enabled"`
	Schedule    SimpleSchedule `json:"schedule"`
	Steps       []SimpleStep   `json:"steps"`
	NextRun     *time.Time     `json:"nextRun,omitempty"`
	Status      SimpleStatus   `json:"status"`
	CurrentStep int            `json:"currentStep,omitempty"`
	LastRun     LastRun        `json:"lastRun,omitempty"`
	CreatedAt   time.Time      `json:"createdAt"`
	UpdatedAt   time.Time      `json:"updatedAt"`
}

type simpleFile struct {
	Automations []SimpleAutomation `json:"automations"`
}

// Scheduler is intentionally an in-process scheduler: cancelling the app
// context or calling Close cancels all active work and no occurrence is ever
// replayed after the application has been closed.
type Scheduler struct {
	store     *store.Store
	executors *executor.Service
	path      string
	ctx       context.Context
	cancel    context.CancelFunc
	mu        sync.RWMutex
	items     map[string]SimpleAutomation
	running   map[string]context.CancelFunc
	seen      map[string]struct{}
	wg        sync.WaitGroup
}

func NewScheduler(parent context.Context, root string, repository *store.Store, executors *executor.Service) (*Scheduler, error) {
	ctx, cancel := context.WithCancel(parent)
	s := &Scheduler{store: repository, executors: executors, path: filepath.Join(root, "automations.json"), ctx: ctx, cancel: cancel, items: map[string]SimpleAutomation{}, running: map[string]context.CancelFunc{}, seen: map[string]struct{}{}}
	if err := s.load(); err != nil {
		cancel()
		return nil, err
	}
	// Startup deliberately calculates only future occurrences. A once job
	// already in the past becomes Missed; daily/weekly occurrences are never
	// backfilled.
	if err := s.normalize(time.Now()); err != nil {
		cancel()
		return nil, err
	}
	return s, nil
}

func (s *Scheduler) Start() {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-s.ctx.Done():
				return
			case now := <-ticker.C:
				s.process(now)
			}
		}
	}()
}

func (s *Scheduler) Close() {
	s.cancel()
	s.mu.Lock()
	for _, cancel := range s.running {
		cancel()
	}
	s.mu.Unlock()
	s.wg.Wait()
}

func (s *Scheduler) List() []SimpleAutomation {
	s.mu.RLock()
	items := make([]SimpleAutomation, 0, len(s.items))
	for _, item := range s.items {
		items = append(items, cloneAutomation(item))
	}
	s.mu.RUnlock()
	sort.Slice(items, func(i, j int) bool { return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name) })
	return items
}

func (s *Scheduler) Get(automationID string) (SimpleAutomation, bool) {
	s.mu.RLock()
	item, ok := s.items[automationID]
	s.mu.RUnlock()
	return cloneAutomation(item), ok
}

func (s *Scheduler) Save(item SimpleAutomation) (SimpleAutomation, error) {
	if err := validateSimple(item); err != nil {
		return SimpleAutomation{}, err
	}
	now := time.Now()
	if item.ID == "" {
		item.ID, item.CreatedAt = id.New("automation"), now.UTC()
	}
	for i := range item.Steps {
		if item.Steps[i].ID == "" {
			item.Steps[i].ID = id.New("automation-step")
		}
	}
	item.UpdatedAt = now.UTC()
	if item.Enabled {
		next, err := nextSimpleOccurrence(item.Schedule, now)
		if err != nil {
			return SimpleAutomation{}, err
		}
		if next == nil && item.Schedule.Type == ScheduleOnce {
			item.Enabled, item.NextRun, item.Status = false, nil, StatusMissed
			item.LastRun.Status, item.LastRun.Error = StatusMissed, "scheduled time has already passed"
		} else {
			item.NextRun, item.Status = next, StatusScheduled
		}
	} else {
		item.NextRun, item.Status = nil, StatusDisabled
	}
	s.mu.Lock()
	if existing, ok := s.items[item.ID]; ok && existing.Status == StatusRunning {
		s.mu.Unlock()
		return SimpleAutomation{}, fmt.Errorf("automation is currently running")
	}
	s.items[item.ID] = cloneAutomation(item)
	err := s.persistLocked()
	s.mu.Unlock()
	return item, err
}

func (s *Scheduler) Delete(automationID string) error {
	s.mu.Lock()
	if _, running := s.running[automationID]; running {
		s.mu.Unlock()
		return fmt.Errorf("cancel the running automation before deleting it")
	}
	delete(s.items, automationID)
	err := s.persistLocked()
	s.mu.Unlock()
	return err
}

func (s *Scheduler) RunNow(automationID string) error {
	return s.start(automationID, "manual", time.Now())
}

func (s *Scheduler) Cancel(automationID string) error {
	s.mu.RLock()
	cancel, running := s.running[automationID]
	s.mu.RUnlock()
	if !running {
		return fmt.Errorf("automation is not running")
	}
	cancel()
	return nil
}

func (s *Scheduler) process(now time.Time) {
	s.mu.RLock()
	var due []struct {
		id string
		at time.Time
	}
	for id, item := range s.items {
		if item.Enabled && item.NextRun != nil && !item.NextRun.After(now) {
			due = append(due, struct {
				id string
				at time.Time
			}{id, *item.NextRun})
		}
	}
	s.mu.RUnlock()
	for _, item := range due {
		_ = s.start(item.id, "scheduled", item.at)
	}
}

func (s *Scheduler) start(automationID, trigger string, occurrence time.Time) error {
	s.mu.Lock()
	item, ok := s.items[automationID]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("automation not found")
	}
	if _, running := s.running[automationID]; running {
		s.mu.Unlock()
		return fmt.Errorf("automation is already running")
	}
	key := automationID + ":" + occurrence.In(time.Local).Format("2006-01-02T15:04")
	if trigger == "scheduled" {
		if _, seen := s.seen[key]; seen {
			s.mu.Unlock()
			return nil
		}
		s.seen[key] = struct{}{}
	}
	ctx, cancel := context.WithCancel(s.ctx)
	s.running[automationID] = cancel
	item.Status, item.CurrentStep, item.NextRun = StatusRunning, 0, nil
	started := time.Now().UTC()
	item.LastRun = LastRun{StartedAt: &started, Status: StatusRunning, Trigger: trigger}
	s.items[automationID] = item
	_ = s.persistLocked()
	s.mu.Unlock()
	s.wg.Add(1)
	go func() { defer s.wg.Done(); s.execute(ctx, automationID) }()
	return nil
}

func (s *Scheduler) execute(ctx context.Context, automationID string) {
	s.mu.RLock()
	item, ok := s.items[automationID]
	s.mu.RUnlock()
	if !ok {
		return
	}
	failedStep, runErr := "", error(nil)
	previews := make([]string, 0, len(item.Steps))
	if _, err := s.store.GetSession(ctx, item.SessionID); err != nil {
		runErr = fmt.Errorf("session %q was not found: %w", item.SessionID, err)
	}
	for i, step := range item.Steps {
		if runErr != nil {
			break
		}
		if _, err := s.store.GetExecutor(ctx, step.ExecutorID); err != nil {
			failedStep, runErr = step.ID, fmt.Errorf("executor %q was not found: %w", step.ExecutorID, err)
			break
		}
		for attempt := 1; ; attempt++ {
			s.setCurrentStep(automationID, i+1)
			result, err := s.executors.RunAutomationStep(ctx, id.New("automation-work"), item.SessionID, step.ExecutorID, step.Prompt, 120, 36)
			if err == nil {
				if result.Output != "" {
					previews = append(previews, result.Output)
				}
				break
			}
			if ctx.Err() != nil {
				failedStep, runErr = step.ID, fmt.Errorf("step %d (%s): %w", i+1, step.ExecutorID, err)
				break
			}
			// A PTY can temporarily be held by the local operator (or be in
			// the middle of starting). Keep this occurrence alive and retry
			// rather than silently deferring it to the next day. Cancel always
			// interrupts this wait through the same execution context.
			s.setRetrying(automationID, i+1, attempt, err)
			timer := time.NewTimer(automationRetryDelay)
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				failedStep, runErr = step.ID, fmt.Errorf("step %d (%s): %w", i+1, step.ExecutorID, ctx.Err())
			case <-timer.C:
			}
			if runErr != nil {
				break
			}
		}
		if runErr != nil {
			break
		}
	}
	finished := time.Now().UTC()
	s.mu.Lock()
	current, exists := s.items[automationID]
	delete(s.running, automationID)
	if !exists {
		s.mu.Unlock()
		return
	}
	current.CurrentStep = 0
	current.LastRun.FinishedAt, current.LastRun.OutputPreview = &finished, previews
	if ctx.Err() != nil {
		current.Status, current.LastRun.Status, current.LastRun.Error = StatusCanceled, StatusCanceled, "canceled"
	} else if runErr != nil {
		current.Status, current.LastRun.Status = StatusFailed, StatusFailed
		current.LastRun.FailedStepID, current.LastRun.Error = failedStep, runErr.Error()
	} else {
		current.Status, current.LastRun.Status, current.LastRun.Error = StatusCompleted, StatusCompleted, ""
	}
	if current.Schedule.Type == ScheduleOnce {
		current.Enabled, current.NextRun = false, nil
		if current.Status == StatusCompleted || current.Status == StatusFailed || current.Status == StatusCanceled {
			current.Status = StatusDisabled
		}
	} else if current.Enabled {
		next, err := nextSimpleOccurrence(current.Schedule, finished)
		if err != nil {
			current.Status, current.LastRun.Status, current.LastRun.Error = StatusFailed, StatusFailed, err.Error()
		} else {
			current.NextRun = next
			current.Status = StatusScheduled
		}
	}
	current.UpdatedAt = finished
	s.items[automationID] = current
	_ = s.persistLocked()
	s.mu.Unlock()
}

func (s *Scheduler) setCurrentStep(automationID string, step int) {
	s.mu.Lock()
	if item, ok := s.items[automationID]; ok {
		item.CurrentStep = step
		s.items[automationID] = item
		_ = s.persistLocked()
	}
	s.mu.Unlock()
}

func (s *Scheduler) setRetrying(automationID string, step, attempt int, cause error) {
	s.mu.Lock()
	if item, ok := s.items[automationID]; ok {
		item.CurrentStep, item.Status, item.LastRun.Status = step, StatusRunning, StatusRunning
		item.LastRun.RetryCount = attempt
		item.LastRun.Error = fmt.Sprintf("Retrying step %d in %s: %v", step, automationRetryDelay, cause)
		item.UpdatedAt = time.Now().UTC()
		s.items[automationID] = item
		_ = s.persistLocked()
	}
	s.mu.Unlock()
}

func (s *Scheduler) normalize(now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	for key, item := range s.items {
		if !item.Enabled {
			item.Status, item.NextRun = StatusDisabled, nil
			s.items[key] = item
			continue
		}
		next, err := nextSimpleOccurrence(item.Schedule, now)
		if err != nil {
			return err
		}
		if next == nil && item.Schedule.Type == ScheduleOnce {
			item.Enabled, item.NextRun, item.Status = false, nil, StatusMissed
			item.LastRun.Status, item.LastRun.Error = StatusMissed, "scheduled time passed while Session Hub was closed"
		} else {
			item.NextRun, item.Status = next, StatusScheduled
		}
		s.items[key], changed = item, true
	}
	if changed {
		return s.persistLocked()
	}
	return nil
}

func (s *Scheduler) load() error {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read automations: %w", err)
	}
	var file simpleFile
	if err := json.Unmarshal(data, &file); err != nil {
		return fmt.Errorf("decode automations: %w", err)
	}
	for _, item := range file.Automations {
		if item.ID != "" {
			s.items[item.ID] = cloneAutomation(item)
		}
	}
	return nil
}

func (s *Scheduler) persistLocked() error {
	items := make([]SimpleAutomation, 0, len(s.items))
	for _, item := range s.items {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	data, err := json.MarshalIndent(simpleFile{Automations: items}, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func validateSimple(item SimpleAutomation) error {
	if strings.TrimSpace(item.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if strings.TrimSpace(item.SessionID) == "" {
		return fmt.Errorf("session is required")
	}
	if len(item.Steps) == 0 {
		return fmt.Errorf("at least one step is required")
	}
	for i, step := range item.Steps {
		if strings.TrimSpace(step.ExecutorID) == "" || strings.TrimSpace(step.Prompt) == "" {
			return fmt.Errorf("step %d needs an executor and prompt", i+1)
		}
	}
	if _, err := nextSimpleOccurrence(item.Schedule, time.Now()); err != nil && !(item.Schedule.Type == ScheduleOnce && strings.Contains(err.Error(), "past")) {
		return err
	}
	return nil
}

func nextSimpleOccurrence(schedule SimpleSchedule, after time.Time) (*time.Time, error) {
	parts := strings.Split(strings.TrimSpace(schedule.Time), ":")
	if len(parts) != 2 {
		return nil, fmt.Errorf("time must use HH:MM")
	}
	var hour, minute int
	if _, err := fmt.Sscanf(schedule.Time, "%d:%d", &hour, &minute); err != nil || hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return nil, fmt.Errorf("time must use HH:MM")
	}
	localAfter := after.In(time.Local)
	makeAt := func(day time.Time) time.Time {
		return time.Date(day.Year(), day.Month(), day.Day(), hour, minute, 0, 0, time.Local)
	}
	switch schedule.Type {
	case ScheduleOnce:
		day, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(schedule.Date), time.Local)
		if err != nil {
			return nil, fmt.Errorf("date must use YYYY-MM-DD")
		}
		candidate := makeAt(day)
		if !candidate.After(localAfter) {
			return nil, nil
		}
		return &candidate, nil
	case ScheduleDaily:
		candidate := makeAt(localAfter)
		if !candidate.After(localAfter) {
			candidate = makeAt(localAfter.AddDate(0, 0, 1))
		}
		return &candidate, nil
	case ScheduleWeekly:
		if len(schedule.DaysOfWeek) == 0 {
			return nil, fmt.Errorf("select at least one day of week")
		}
		allowed := map[time.Weekday]bool{}
		for _, day := range schedule.DaysOfWeek {
			allowed[day] = true
		}
		for i := 0; i <= 7; i++ {
			candidate := makeAt(localAfter.AddDate(0, 0, i))
			if allowed[candidate.Weekday()] && candidate.After(localAfter) {
				return &candidate, nil
			}
		}
	}
	return nil, fmt.Errorf("schedule type must be once, daily, or weekly")
}

func cloneAutomation(item SimpleAutomation) SimpleAutomation {
	item.Steps = append([]SimpleStep(nil), item.Steps...)
	item.Schedule.DaysOfWeek = append([]time.Weekday(nil), item.Schedule.DaysOfWeek...)
	item.LastRun.OutputPreview = append([]string(nil), item.LastRun.OutputPreview...)
	return item
}
