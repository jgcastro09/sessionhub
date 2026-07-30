package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/jgcastro09/sessionhub/internal/domain"
)

func TestMigrationsAndRecovery(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "sessionhub.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()

	session, err := s.SaveSession(ctx, domain.Session{Name: "test", Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	cfg := domain.ExecutorConfig{ID: "exec_test", Name: "manual", Command: "manual"}
	if err := s.SaveExecutor(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	inst, err := s.CreateInstance(ctx, domain.Instance{
		SessionID: session.ID, ExecutorID: cfg.ID, State: domain.StateRunning,
	})
	if err != nil {
		t.Fatal(err)
	}
	n, err := s.Recover(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n < 1 {
		t.Fatalf("expected recovered records, got %d", n)
	}
	got, err := s.GetInstance(ctx, inst.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != domain.StateInterrupted {
		t.Fatalf("got %s, want interrupted", got.State)
	}

	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
}

func TestExecutorStartsEmptyAndSecretsRedact(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	list, err := s.ListExecutors(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("new database contains %d executors", len(list))
	}

	cfg := domain.ExecutorConfig{
		Name:        "user supplied",
		Command:     "external",
		Environment: []domain.SecretEnv{{Name: "TOKEN", Value: "abc", Secret: true}},
	}
	if err := s.SaveExecutor(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	list, err = s.ListExecutors(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("list executors: %v %#v", err, list)
	}
	if got := list[0].Redacted().Environment[0].Value; got != "***" {
		t.Fatalf("secret leaked through redaction: %q", got)
	}
}
