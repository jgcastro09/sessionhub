package terminal

import (
	"context"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nodestage/sessionhub/internal/domain"
)

type memoryHistory struct {
	mu      sync.Mutex
	records [][]byte
}

func (m *memoryHistory) AppendTerminal(_ context.Context, _, _ string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records = append(m.records, append([]byte(nil), data...))
	return nil
}

func TestPTYInteractiveUnicodeAndResize(t *testing.T) {
	var cfg domain.ExecutorConfig
	if runtime.GOOS == "windows" {
		cfg = domain.ExecutorConfig{
			Name: "test", Command: "cmd.exe",
			Args: []string{"/d", "/q", "/k", "echo READY"},
		}
	} else {
		cfg = domain.ExecutorConfig{
			Name: "test", Command: "sh",
			Args: []string{"-c", "printf 'READY\\n'; while IFS= read -r line; do printf 'ECHO:%s\\n' \"$line\"; done"},
		}
	}
	history := &memoryHistory{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := make(chan Event, 32)
	session, err := Start(ctx, "test", cfg, 80, 24, 100, history, events)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(time.Second)
	if err := session.Resize(100, 30); err != nil {
		t.Fatal(err)
	}
	owner := Owner{Kind: "local", ID: "operator"}
	if err := session.Write(owner, []byte("Olá 世界\r\n")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		screen := session.Snapshot()
		if strings.Contains(screen, "READY") && strings.Contains(screen, "Olá") {
			return
		}
		select {
		case <-events:
		case <-time.After(20 * time.Millisecond):
		}
	}
	t.Fatalf("PTY snapshot did not contain expected output: %q", session.Snapshot())
}

func TestControlLease(t *testing.T) {
	s := &Session{state: domain.StateRunning, owner: Owner{Kind: "local", ID: "one"}}
	if err := s.Acquire(Owner{Kind: "remote", ID: "two"}); err == nil {
		t.Fatal("expected busy lease")
	}
	if err := s.Transfer(Owner{Kind: "local", ID: "one"}, Owner{Kind: "remote", ID: "two"}); err != nil {
		t.Fatal(err)
	}
	if got := s.Owner(); got.Kind != "remote" {
		t.Fatalf("unexpected owner: %#v", got)
	}
}
