package domain

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

type SecretEnv struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Secret bool   `json:"secret"`
}

type RecognitionKind string

const (
	RecognizeProcessExit RecognitionKind = "process_exit"
	RecognizeExitCode    RecognitionKind = "exit_code"
	RecognizeLiteral     RecognitionKind = "literal"
	RecognizePattern     RecognitionKind = "pattern"
	RecognizePrompt      RecognitionKind = "prompt_return"
	RecognizeStable      RecognitionKind = "stable_output"
	RecognizeCommand     RecognitionKind = "command_result"
	RecognizeManual      RecognitionKind = "manual"
	RecognizeTimeout     RecognitionKind = "timeout"
)

type RecognitionRule struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Kind     RecognitionKind `json:"kind"`
	Value    string          `json:"value,omitempty"`
	Outcome  State           `json:"outcome"`
	Priority int             `json:"priority"`
}

func (r RecognitionRule) Validate() error {
	switch r.Kind {
	case RecognizeProcessExit, RecognizeExitCode, RecognizeLiteral,
		RecognizePrompt, RecognizeStable, RecognizeCommand, RecognizeManual,
		RecognizeTimeout:
	case RecognizePattern:
		if _, err := regexp.Compile(r.Value); err != nil {
			return fmt.Errorf("recognition rule %q pattern: %w", r.Name, err)
		}
	default:
		return fmt.Errorf("recognition rule %q has unknown kind %q", r.Name, r.Kind)
	}
	if r.Outcome == "" {
		return fmt.Errorf("recognition rule %q has no outcome", r.Name)
	}
	return nil
}

type ExecutorConfig struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Command string `json:"command"`
	// InstallDir is the absolute path to this executor's own managed folder
	// (executors/<slug>/{bin,config,runtime}/manifest.json) when it was
	// installed through SessionHub's "add a CLI" flow. Empty for executors
	// registered manually against something already on disk elsewhere.
	InstallDir string `json:"install_dir,omitempty"`
	// BinaryName is the bare command name (e.g. "codex") as it was before
	// being resolved to an absolute path inside InstallDir. Kept so an
	// "update the CLI" run can re-resolve Command afterward without
	// guessing a name back out of the resolved path.
	BinaryName    string            `json:"binary_name,omitempty"`
	Args          []string          `json:"args"`
	WorkingDir    string            `json:"working_dir,omitempty"`
	Environment   []SecretEnv       `json:"environment,omitempty"`
	Shell         string            `json:"shell,omitempty"`
	ResumeCommand string            `json:"resume_command,omitempty"`
	ResumeArgs    []string          `json:"resume_args,omitempty"`
	Rules         []RecognitionRule `json:"rules,omitempty"`
	Timeout       time.Duration     `json:"timeout"`
	PromptSuffix  string            `json:"prompt_suffix"`
	Roles         []string          `json:"roles,omitempty"`
	Model         string            `json:"model,omitempty"`
	Tokenizer     string            `json:"tokenizer,omitempty"`
	PriceID       string            `json:"price_id,omitempty"`
	CreatedAt     time.Time         `json:"created_at"`
	UpdatedAt     time.Time         `json:"updated_at"`
}

func (c ExecutorConfig) Validate() error {
	if strings.TrimSpace(c.Name) == "" {
		return fmt.Errorf("executor display name is required")
	}
	if strings.TrimSpace(c.Command) == "" {
		return fmt.Errorf("executor command is required")
	}
	if c.Timeout < 0 {
		return fmt.Errorf("executor timeout cannot be negative")
	}
	seen := make(map[string]bool, len(c.Environment))
	for _, env := range c.Environment {
		key := strings.ToUpper(strings.TrimSpace(env.Name))
		if key == "" {
			return fmt.Errorf("environment variable name is required")
		}
		if seen[key] {
			return fmt.Errorf("environment variable %q is duplicated", env.Name)
		}
		seen[key] = true
	}
	for _, rule := range c.Rules {
		if err := rule.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func (c ExecutorConfig) Redacted() ExecutorConfig {
	out := c
	out.Environment = append([]SecretEnv(nil), c.Environment...)
	for i := range out.Environment {
		if out.Environment[i].Secret {
			out.Environment[i].Value = "***"
		}
	}
	return out
}

type Session struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	Workspace      string          `json:"workspace"`
	ActiveInstance string          `json:"active_instance,omitempty"`
	Settings       json.RawMessage `json:"settings,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

func (s Session) Validate() error {
	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("session name is required")
	}
	if strings.TrimSpace(s.Workspace) == "" {
		return fmt.Errorf("session workspace is required")
	}
	return nil
}

// sessionSettings is the shape persisted in Session.Settings. A session
// carries no conversation "context" of its own — it's just a workspace plus
// which CLIs (Executors) are grouped together as tabs under it.
type sessionSettings struct {
	ExecutorIDs []string `json:"executor_ids,omitempty"`
}

// ExecutorIDs returns the Executors grouped under this session (one tab
// each), in the order they were assigned.
func (s Session) ExecutorIDs() []string {
	if len(s.Settings) == 0 {
		return nil
	}
	var settings sessionSettings
	_ = json.Unmarshal(s.Settings, &settings)
	return settings.ExecutorIDs
}

// SetExecutorIDs replaces the Executors grouped under this session.
func (s *Session) SetExecutorIDs(ids []string) {
	data, err := json.Marshal(sessionSettings{ExecutorIDs: ids})
	if err != nil {
		return
	}
	s.Settings = data
}

type Instance struct {
	ID         string     `json:"id"`
	SessionID  string     `json:"session_id"`
	ExecutorID string     `json:"executor_id"`
	State      State      `json:"state"`
	ExitCode   *int       `json:"exit_code,omitempty"`
	Error      string     `json:"error,omitempty"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	EndedAt    *time.Time `json:"ended_at,omitempty"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

type GlobalContext struct {
	SessionID       string          `json:"session_id"`
	Goal            string          `json:"goal,omitempty"`
	OriginalRequest string          `json:"original_request,omitempty"`
	Progress        string          `json:"progress,omitempty"`
	Decisions       []string        `json:"decisions,omitempty"`
	Constraints     []string        `json:"constraints,omitempty"`
	RelevantFiles   []string        `json:"relevant_files,omitempty"`
	ChangedFiles    []string        `json:"changed_files,omitempty"`
	GitState        json.RawMessage `json:"git_state,omitempty"`
	CompletedTasks  []string        `json:"completed_tasks,omitempty"`
	CurrentTask     string          `json:"current_task,omitempty"`
	Pending         []string        `json:"pending,omitempty"`
	Validation      []string        `json:"validation,omitempty"`
	ExecutorResults []string        `json:"executor_results,omitempty"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

type Checkpoint struct {
	ID        string          `json:"id"`
	SessionID string          `json:"session_id"`
	Name      string          `json:"name"`
	Automatic bool            `json:"automatic"`
	Snapshot  json.RawMessage `json:"snapshot"`
	CreatedAt time.Time       `json:"created_at"`
}

type Precision string

const (
	PrecisionExact       Precision = "exact"
	PrecisionEstimated   Precision = "estimated"
	PrecisionApproximate Precision = "approximate"
)

type Metric struct {
	ID             string    `json:"id"`
	SessionID      string    `json:"session_id,omitempty"`
	ExecutorID     string    `json:"executor_id,omitempty"`
	InstanceID     string    `json:"instance_id,omitempty"`
	AutomationID   string    `json:"automation_id,omitempty"`
	PipelineStepID string    `json:"pipeline_step_id,omitempty"`
	Model          string    `json:"model,omitempty"`
	InputTokens    int64     `json:"input_tokens"`
	OutputTokens   int64     `json:"output_tokens"`
	CacheRead      int64     `json:"cache_read"`
	CacheWrite     int64     `json:"cache_write"`
	PromptCount    int64     `json:"prompt_count"`
	ResponseCount  int64     `json:"response_count"`
	Duration       int64     `json:"duration_ms"`
	CostMicrosUSD  int64     `json:"cost_micros_usd"`
	Precision      Precision `json:"precision"`
	PriceVersion   string    `json:"price_version,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

func (m Metric) TotalTokens() int64 { return m.InputTokens + m.OutputTokens }

type Price struct {
	ID               string    `json:"id"`
	Model            string    `json:"model"`
	Version          string    `json:"version"`
	InputMicros      int64     `json:"input_micros_per_million"`
	OutputMicros     int64     `json:"output_micros_per_million"`
	CacheReadMicros  int64     `json:"cache_read_micros_per_million"`
	CacheWriteMicros int64     `json:"cache_write_micros_per_million"`
	ZeroCostExplicit bool      `json:"zero_cost_explicit"`
	EffectiveAt      time.Time `json:"effective_at"`
}

type Budget struct {
	InputTokens   int64         `json:"input_tokens,omitempty"`
	OutputTokens  int64         `json:"output_tokens,omitempty"`
	TotalTokens   int64         `json:"total_tokens,omitempty"`
	CostMicrosUSD int64         `json:"cost_micros_usd,omitempty"`
	Duration      time.Duration `json:"duration,omitempty"`
	Attempts      int           `json:"attempts,omitempty"`
	Cycles        int           `json:"cycles,omitempty"`
	Concurrency   int           `json:"concurrency,omitempty"`
	OnExceeded    string        `json:"on_exceeded,omitempty"`
}

func (b Budget) HasObjectiveLimit() bool {
	return b.InputTokens > 0 || b.OutputTokens > 0 || b.TotalTokens > 0 ||
		b.CostMicrosUSD > 0 || b.Duration > 0 || b.Attempts > 0 || b.Cycles > 0
}

type QueueItem struct {
	ID               string          `json:"id"`
	SessionID        string          `json:"session_id"`
	ExecutorID       string          `json:"executor_id"`
	Prompt           string          `json:"prompt"`
	Priority         int             `json:"priority"`
	Dependencies     []string        `json:"dependencies,omitempty"`
	StartCondition   string          `json:"start_condition,omitempty"`
	Timeout          time.Duration   `json:"timeout"`
	MaxAttempts      int             `json:"max_attempts"`
	Attempts         int             `json:"attempts"`
	Budget           Budget          `json:"budget"`
	FailurePolicy    string          `json:"failure_policy,omitempty"`
	NeedsApproval    bool            `json:"needs_approval"`
	Context          json.RawMessage `json:"context,omitempty"`
	OnSuccess        string          `json:"on_success,omitempty"`
	OnFailure        string          `json:"on_failure,omitempty"`
	OnCancel         string          `json:"on_cancel,omitempty"`
	State            State           `json:"state"`
	IdempotencyKey   string          `json:"idempotency_key"`
	CompletionRuleID string          `json:"completion_rule_id,omitempty"`
	Result           json.RawMessage `json:"result,omitempty"`
	Error            string          `json:"error,omitempty"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
}

type ScheduleKind string

const (
	ScheduleOnce     ScheduleKind = "once"
	ScheduleDaily    ScheduleKind = "daily"
	ScheduleWeekdays ScheduleKind = "weekdays"
	ScheduleInterval ScheduleKind = "interval"
)

type MissedPolicy string

const (
	MissedSkip    MissedPolicy = "skip"
	MissedRunOnce MissedPolicy = "run_once"
	MissedAsk     MissedPolicy = "ask"
)

type Schedule struct {
	ID           string          `json:"id"`
	SessionID    string          `json:"session_id"`
	Name         string          `json:"name"`
	Kind         ScheduleKind    `json:"kind"`
	Spec         string          `json:"spec"`
	Timezone     string          `json:"timezone"`
	TargetType   string          `json:"target_type"`
	Target       json.RawMessage `json:"target"`
	Enabled      bool            `json:"enabled"`
	MissedPolicy MissedPolicy    `json:"missed_policy"`
	NextRun      *time.Time      `json:"next_run,omitempty"`
	LastRun      *time.Time      `json:"last_run,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

type StepType string

const (
	StepPrompt      StepType = "prompt"
	StepCommand     StepType = "command"
	StepApproval    StepType = "approval"
	StepCondition   StepType = "condition"
	StepParallel    StepType = "parallel"
	StepConsolidate StepType = "consolidate"
)

type Pipeline struct {
	ID            string          `json:"id"`
	SessionID     string          `json:"session_id"`
	Name          string          `json:"name"`
	State         State           `json:"state"`
	Budget        Budget          `json:"budget"`
	Template      bool            `json:"template"`
	CurrentStepID string          `json:"current_step_id,omitempty"`
	Request       string          `json:"request,omitempty"`
	Result        json.RawMessage `json:"result,omitempty"`
	Error         string          `json:"error,omitempty"`
	StartedAt     *time.Time      `json:"started_at,omitempty"`
	EndedAt       *time.Time      `json:"ended_at,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

type PipelineStep struct {
	ID             string          `json:"id"`
	PipelineID     string          `json:"pipeline_id"`
	Name           string          `json:"name"`
	Type           StepType        `json:"type"`
	State          State           `json:"state"`
	ExecutorID     string          `json:"executor_id,omitempty"`
	Config         json.RawMessage `json:"config"`
	Dependencies   []string        `json:"dependencies,omitempty"`
	ReadOnly       bool            `json:"read_only"`
	Files          []string        `json:"files,omitempty"`
	MaxAttempts    int             `json:"max_attempts"`
	Attempts       int             `json:"attempts"`
	Budget         Budget          `json:"budget"`
	IdempotencyKey string          `json:"idempotency_key"`
	Result         json.RawMessage `json:"result,omitempty"`
	Error          string          `json:"error,omitempty"`
	StartedAt      *time.Time      `json:"started_at,omitempty"`
	EndedAt        *time.Time      `json:"ended_at,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

type Approval struct {
	ID          string     `json:"id"`
	SessionID   string     `json:"session_id"`
	TargetType  string     `json:"target_type"`
	TargetID    string     `json:"target_id"`
	Reason      string     `json:"reason"`
	State       State      `json:"state"`
	DecidedBy   string     `json:"decided_by,omitempty"`
	Decision    string     `json:"decision,omitempty"`
	RequestedAt time.Time  `json:"requested_at"`
	DecidedAt   *time.Time `json:"decided_at,omitempty"`
}

type Watcher struct {
	ID            string          `json:"id"`
	SessionID     string          `json:"session_id"`
	Name          string          `json:"name"`
	Kind          string          `json:"kind"`
	Workspace     string          `json:"workspace"`
	Pattern       string          `json:"pattern"`
	Action        json.RawMessage `json:"action"`
	Enabled       bool            `json:"enabled"`
	Debounce      time.Duration   `json:"debounce"`
	LastEventHash string          `json:"last_event_hash,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

type LogEntry struct {
	ID        string          `json:"id"`
	SessionID string          `json:"session_id,omitempty"`
	Level     string          `json:"level"`
	Kind      string          `json:"kind"`
	Message   string          `json:"message"`
	Fields    json.RawMessage `json:"fields,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}
