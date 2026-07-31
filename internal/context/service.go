package context

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jgcastro09/sessionhub/internal/domain"
	"github.com/jgcastro09/sessionhub/internal/gitstate"
)

type Repository interface {
	GetProject(context.Context, string) (domain.Project, error)
	LoadContext(context.Context, string) (domain.GlobalContext, error)
	ListInstances(context.Context, string) ([]domain.Instance, error)
	AggregateMetrics(context.Context, string) (domain.Metric, error)
	CreateCheckpoint(context.Context, domain.Checkpoint) (domain.Checkpoint, error)
}

type Service struct{ repository Repository }

func New(repository Repository) *Service { return &Service{repository: repository} }

type Snapshot struct {
	Context   domain.GlobalContext `json:"context"`
	Instances []domain.Instance    `json:"instances"`
	Git       *gitstate.State      `json:"git,omitempty"`
	Metrics   domain.Metric        `json:"metrics"`
}

func (s *Service) Checkpoint(
	ctx context.Context,
	projectID, name string,
	automatic bool,
) (domain.Checkpoint, error) {
	project, err := s.repository.GetProject(ctx, projectID)
	if err != nil {
		return domain.Checkpoint{}, err
	}
	global, err := s.repository.LoadContext(ctx, projectID)
	if err != nil {
		return domain.Checkpoint{}, err
	}
	instances, err := s.repository.ListInstances(ctx, projectID)
	if err != nil {
		return domain.Checkpoint{}, err
	}
	metric, err := s.repository.AggregateMetrics(ctx, projectID)
	if err != nil {
		return domain.Checkpoint{}, err
	}
	snapshot := Snapshot{Context: global, Instances: instances, Metrics: metric}
	if state, gitErr := gitstate.Inspect(ctx, project.Root); gitErr == nil {
		snapshot.Git = &state
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return domain.Checkpoint{}, fmt.Errorf("encode checkpoint: %w", err)
	}
	return s.repository.CreateCheckpoint(ctx, domain.Checkpoint{
		ProjectID: projectID, Name: name, Automatic: automatic, Snapshot: data,
	})
}

func ContinuityPackage(global domain.GlobalContext, git *gitstate.State, result string) ([]byte, error) {
	payload := struct {
		Goal            string          `json:"goal,omitempty"`
		OriginalRequest string          `json:"original_request,omitempty"`
		Progress        string          `json:"progress,omitempty"`
		Decisions       []string        `json:"decisions,omitempty"`
		Constraints     []string        `json:"constraints,omitempty"`
		RelevantFiles   []string        `json:"relevant_files,omitempty"`
		ChangedFiles    []string        `json:"changed_files,omitempty"`
		CurrentTask     string          `json:"current_task,omitempty"`
		Pending         []string        `json:"pending,omitempty"`
		Validation      []string        `json:"validation,omitempty"`
		Git             *gitstate.State `json:"git,omitempty"`
		PreviousResult  string          `json:"previous_result,omitempty"`
	}{
		Goal: global.Goal, OriginalRequest: global.OriginalRequest,
		Progress: global.Progress, Decisions: global.Decisions,
		Constraints: global.Constraints, RelevantFiles: global.RelevantFiles,
		ChangedFiles: global.ChangedFiles, CurrentTask: global.CurrentTask,
		Pending: global.Pending, Validation: global.Validation,
		Git: git, PreviousResult: result,
	}
	return json.MarshalIndent(payload, "", "  ")
}
