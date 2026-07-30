package terminal

import (
	"context"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/jgcastro09/sessionhub/internal/domain"
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

func TestSnapshotScrolledShowsHistoryAboveLiveTail(t *testing.T) {
	var cfg domain.ExecutorConfig
	if runtime.GOOS == "windows" {
		cfg = domain.ExecutorConfig{
			Name: "test", Command: "cmd.exe",
			Args: []string{"/d", "/q", "/c", "for /l %i in (1,1,40) do @echo LINE%i"},
		}
	} else {
		cfg = domain.ExecutorConfig{
			Name: "test", Command: "sh",
			Args: []string{"-c", "i=1; while [ $i -le 40 ]; do printf 'LINE%d\\n' $i; i=$((i+1)); done"},
		}
	}
	history := &memoryHistory{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := make(chan Event, 64)
	// A short screen (height 5) forces most of the 40 lines into scrollback.
	session, err := Start(ctx, "test-scroll", cfg, 20, 5, 100, history, events)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(time.Second)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && session.ScrollbackLen() < 20 {
		select {
		case <-events:
		case <-time.After(20 * time.Millisecond):
		}
	}
	if session.ScrollbackLen() < 20 {
		t.Fatalf("expected substantial scrollback, got %d lines: %q", session.ScrollbackLen(), session.Snapshot())
	}

	live := session.SnapshotScrolled(0, 5)
	if live != session.Snapshot() {
		t.Fatalf("offset=0 should equal the live snapshot; got %q vs %q", live, session.Snapshot())
	}
	if strings.Contains(live, "LINE1\n") {
		t.Fatalf("live tail unexpectedly contains an early line: %q", live)
	}

	scrolled := session.SnapshotScrolled(session.ScrollbackLen(), 5)
	if !strings.Contains(scrolled, "LINE1") {
		t.Fatalf("scrolled view should reach back to the earliest lines, got %q", scrolled)
	}
	if scrolled == live {
		t.Fatalf("scrolled view should differ from the live tail")
	}
}

// TestSendKeyLatencyUnderHeavyOutput guards against a real, severe bug:
// SafeEmulator guards SendKey (input) and Write (output, called from
// readOutput for every PTY read) with the same exclusive lock. A child
// process producing continuous rapid output (very common for a streaming
// AI CLI) could starve SendKey behind a constant stream of Write() calls,
// making keystrokes feel like they don't register until several more keys
// are pressed — reported as "impossible to work" on both Windows and macOS.
// readOutput batches PTY reads over a short window before each Write() to
// bound how often that lock gets re-acquired; this measures that the fix
// actually keeps SendKey latency low while the child is chatty.
func TestSendKeyLatencyUnderHeavyOutput(t *testing.T) {
	// Bounded (not `while true`/`for /l %i in ()`) so the test can't hang
	// forever if a kill signal is missed for some reason — small enough
	// that even a full backlog left in the OS pty buffer after killing
	// the process drains near-instantly, but still enough chatter to
	// stress SendKey/render latency for the ~1s the test measures.
	var cfg domain.ExecutorConfig
	if runtime.GOOS == "windows" {
		cfg = domain.ExecutorConfig{
			Name: "chatty", Command: "cmd.exe",
			Args: []string{"/d", "/q", "/c", "for /l %i in (1,1,300000) do @echo " + strings.Repeat("x", 120)},
		}
	} else {
		cfg = domain.ExecutorConfig{
			Name: "chatty", Command: "sh",
			Args: []string{"-c", "i=0; while [ $i -lt 300000 ]; do printf '" + strings.Repeat("x", 120) + "\\n'; i=$((i+1)); done"},
		}
	}
	history := &memoryHistory{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := make(chan Event, 4096)
	session, err := Start(ctx, "chatty", cfg, 80, 24, 1000, history, events)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(time.Second)

	// Let output ramp up to a steady, heavy rate before measuring.
	time.Sleep(300 * time.Millisecond)

	owner := Owner{Kind: "local", ID: "operator"}
	const n = 300
	var total time.Duration
	var max time.Duration
	for i := 0; i < n; i++ {
		key := uv.KeyPressEvent(uv.Key{Code: 'a'})
		start := time.Now()
		if err := session.SendKey(owner, key); err != nil {
			t.Fatal(err)
		}
		elapsed := time.Since(start)
		total += elapsed
		if elapsed > max {
			max = elapsed
		}
	}
	avg := total / n
	t.Logf("SendKey latency under heavy chatty output: avg=%v max=%v (n=%d)", avg, max, n)
	// Guards against the severe regression this reproduces (SendKey
	// blocking for seconds behind readOutput's Write calls) rather than
	// asserting a tight perf budget — shared CI runners are noisy, and
	// this test's own chatty child already competes hard for the same
	// CPU. 300ms is still tiny next to what "impossible to work" looked
	// like, but well clear of ordinary scheduling jitter.
	if max > 300*time.Millisecond {
		t.Errorf("SendKey latency too high under heavy output: max=%v (want < 300ms) — likely emulator lock contention with readOutput's Write calls", max)
	}
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
