package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/jgcastro09/sessionhub/internal/domain"
)

func automationStore(t *testing.T) (*Store, domain.Project, domain.ExecutorConfig) {
	t.Helper()
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "automation.db"))
	if err != nil {
		t.Fatal(err)
	}
	project, err := s.SaveProject(ctx, domain.Project{Name: "automation", Root: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	executor := domain.ExecutorConfig{ID: "exec_manual", Name: "manual", Command: "external"}
	if err := s.SaveExecutor(ctx, executor); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, project, executor
}

func TestQueueClaimIsIdempotent(t *testing.T) {
	s, project, executor := automationStore(t)
	ctx := context.Background()
	item, err := s.Enqueue(ctx, domain.QueueItem{
		ProjectID: project.ID, ExecutorID: executor.ID, Prompt: "work",
		IdempotencyKey: "same-effect",
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := s.ClaimNextQueue(ctx, project.ID)
	if err != nil || claimed == nil || claimed.ID != item.ID {
		t.Fatalf("claim: %#v %v", claimed, err)
	}
	again, err := s.ClaimNextQueue(ctx, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if again != nil {
		t.Fatalf("duplicate claim: %#v", again)
	}
	if err := s.CompleteQueue(ctx, item.ID, domain.StateSucceeded, "rule", nil, ""); err != nil {
		t.Fatal(err)
	}
	again, err = s.ClaimNextQueue(ctx, project.ID)
	if err != nil || again != nil {
		t.Fatalf("completed item replayed: %#v %v", again, err)
	}
}

func TestQueueRetryUsesANewEffectReceipt(t *testing.T) {
	s, project, executor := automationStore(t)
	ctx := context.Background()
	item, err := s.Enqueue(ctx, domain.QueueItem{
		ProjectID: project.ID, ExecutorID: executor.ID, Prompt: "retry",
		IdempotencyKey: "retry-effect", MaxAttempts: 2, FailurePolicy: "retry",
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.ClaimNextQueue(ctx, project.ID)
	if err != nil || first == nil || first.Attempts != 1 {
		t.Fatalf("first claim: %#v %v", first, err)
	}
	if err := s.CompleteQueue(ctx, item.ID, domain.StateFailed, "exit", nil, "failed"); err != nil {
		t.Fatal(err)
	}
	second, err := s.ClaimNextQueue(ctx, project.ID)
	if err != nil || second == nil || second.Attempts != 2 {
		t.Fatalf("second claim: %#v %v", second, err)
	}
	if err := s.CompleteQueue(ctx, item.ID, domain.StateFailed, "exit", nil, "failed again"); err != nil {
		t.Fatal(err)
	}
	third, err := s.ClaimNextQueue(ctx, project.ID)
	if err != nil || third != nil {
		t.Fatalf("retry limit ignored: %#v %v", third, err)
	}
}

func TestPipelineRejectsCycle(t *testing.T) {
	steps := []domain.PipelineStep{
		{ID: "a", Name: "a", MaxAttempts: 1, Dependencies: []string{"b"}},
		{ID: "b", Name: "b", MaxAttempts: 1, Dependencies: []string{"a"}},
	}
	if err := ValidatePipeline(steps); err == nil {
		t.Fatal("expected dependency cycle error")
	}
}

func TestWorkspaceLockConflict(t *testing.T) {
	s, project, _ := automationStore(t)
	ctx := context.Background()
	if err := s.AcquireWorkspaceLock(ctx, project.Root, "one", "read", []string{"a.go"}); err != nil {
		t.Fatal(err)
	}
	if err := s.AcquireWorkspaceLock(ctx, project.Root, "two", "read", []string{"a.go"}); err != nil {
		t.Fatal(err)
	}
	if err := s.AcquireWorkspaceLock(ctx, project.Root, "three", "write", []string{"a.go"}); err == nil {
		t.Fatal("expected write conflict")
	}
	if err := s.AcquireWorkspaceLock(ctx, project.Root, "three", "write", []string{"b.go"}); err != nil {
		t.Fatal(err)
	}
}
