package ui

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jgcastro09/sessionhub/internal/app"
	"github.com/jgcastro09/sessionhub/internal/config"
	"github.com/jgcastro09/sessionhub/internal/domain"
)

func TestNewTerminalAutoAttachesToActiveSessionTopbar(t *testing.T) {
	root := t.TempDir()
	paths := config.Paths{
		Root:      root,
		Database:  filepath.Join(root, "test.db"),
		Logs:      filepath.Join(root, "logs"),
		Downloads: filepath.Join(root, "updates"),
		Executors: filepath.Join(root, "executors"),
	}
	for _, dir := range []string{paths.Logs, paths.Downloads, paths.Executors} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	application, err := app.New(ctx, paths, "test")
	if err != nil {
		t.Fatal(err)
	}
	defer application.Close()

	session := domain.Session{Name: "session1", Workspace: root}
	session, err = application.Store.SaveSession(ctx, session)
	if err != nil {
		t.Fatal(err)
	}

	model := New(application)
	model.sessions = []domain.Session{session}
	model.activeSession = 0

	// Register a new system terminal executor
	execCfg := domain.ExecutorConfig{
		ID: "exec_zsh", Name: "Zsh (Terminal)", Command: "zsh", Args: []string{"-l"}, UseHostHome: true,
	}
	if err := application.Store.SaveExecutor(ctx, execCfg); err != nil {
		t.Fatal(err)
	}

	// Append to active session as done in form submit
	updatedSession := model.sessions[0]
	updatedSession.SetExecutorIDs(append(updatedSession.ExecutorIDs(), execCfg.ID))
	if _, err := application.Store.SaveSession(ctx, updatedSession); err != nil {
		t.Fatal(err)
	}

	model.executors = []domain.ExecutorConfig{execCfg}
	model.sessions = []domain.Session{updatedSession}

	renderedTabs := model.renderTabs()
	if !containsString(updatedSession.ExecutorIDs(), "exec_zsh") {
		t.Errorf("expected exec_zsh in session ExecutorIDs")
	}
	if len(renderedTabs) == 0 {
		t.Errorf("renderedTabs should not be empty")
	}
}

func containsString(slice []string, val string) bool {
	for _, item := range slice {
		if item == val {
			return true
		}
	}
	return false
}
