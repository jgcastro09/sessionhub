package integration

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jgcastro09/sessionhub/internal/domain"
	"github.com/jgcastro09/sessionhub/internal/terminal"
)

func TestTUIStartsInRealPTYAndQuits(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test builds and starts the TUI")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	events := make(chan terminal.Event, 128)
	session, err := terminal.Start(
		context.Background(),
		"tui-integration",
		domain.ExecutorConfig{
			Name: "Session Hub integration",
			Command: "go",
			Args: []string{"run", "./cmd/sessionhub"},
			WorkingDir: root,
			Environment: []domain.SecretEnv{{
				Name: "SESSIONHUB_DATA_DIR", Value: t.TempDir(),
			}},
		},
		100, 30, 100, nil, events,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(2 * time.Second)

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(session.Snapshot(), "SESSION HUB") {
			if err := session.Write(terminal.Owner{Kind: "local", ID: "operator"}, []byte("q")); err != nil {
				t.Fatal(err)
			}
			exitDeadline := time.Now().Add(10 * time.Second)
			for time.Now().Before(exitDeadline) {
				if state := session.State(); state == domain.StateFinished {
					return
				}
				time.Sleep(20 * time.Millisecond)
			}
			t.Fatalf("TUI did not quit after q; state=%s", session.State())
		}
		select {
		case event := <-events:
			if event.Kind == terminal.EventError && event.Err != nil {
				t.Fatal(event.Err)
			}
		case <-time.After(50 * time.Millisecond):
		}
	}
	t.Fatalf("TUI did not render expected header; snapshot=%q", session.Snapshot())
}
