package ui

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/jgcastro09/sessionhub/internal/app"
	"github.com/jgcastro09/sessionhub/internal/config"
	"github.com/jgcastro09/sessionhub/internal/domain"
)

// TestViewLatencyUnderHeavyOutput measures how long Model.View() takes while
// the focused terminal's child process is producing continuous rapid
// output — the scenario reported as "keys don't register until you press
// another one," which happens whenever a chatty CLI (any AI coding agent
// mid-response) is redrawing while the user tries to type. Bubble Tea calls
// View() synchronously after every Update(), so a slow View() here directly
// delays the next keystroke from being processed at all.
func TestViewLatencyUnderHeavyOutput(t *testing.T) {
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

	// Bounded so the test can't hang forever if a kill signal is missed —
	// small enough that even a full backlog left in the OS pty buffer
	// after killing the process drains near-instantly.
	var command string
	var args []string
	if runtime.GOOS == "windows" {
		command, args = "cmd.exe", []string{"/d", "/q", "/c", "for /l %i in (1,1,300000) do @echo " + strings.Repeat("x", 120)}
	} else {
		command, args = "sh", []string{"-c", "i=0; while [ $i -lt 300000 ]; do printf '" + strings.Repeat("x", 120) + "\\n'; i=$((i+1)); done"}
	}
	execA := domain.ExecutorConfig{ID: "exec_a", Name: "Chatty", Command: command, Args: args}
	if err := application.Store.SaveExecutor(ctx, execA); err != nil {
		t.Fatal(err)
	}
	project := domain.Project{Name: "teste", Root: root}
	project.SetExecutorIDs([]string{"exec_a"})
	if _, err := application.Store.SaveProject(ctx, project); err != nil {
		t.Fatal(err)
	}

	var model tea.Model = New(application)
	step := func(msg tea.Msg) tea.Cmd {
		var cmd tea.Cmd
		model, cmd = model.(Model).Update(msg)
		return cmd
	}
	var drain func(cmd tea.Cmd)
	drain = func(cmd tea.Cmd) {
		if cmd == nil {
			return
		}
		msg := cmd()
		if msg == nil {
			return
		}
		switch msg := msg.(type) {
		case tickMsg:
			return
		case tea.BatchMsg:
			for _, c := range msg {
				if c == nil {
					continue
				}
				if inner := c(); inner != nil {
					if _, isTick := inner.(tickMsg); isTick {
						continue
					}
					drain(step(inner))
				}
			}
		default:
			drain(step(msg))
		}
	}

	drain(step(tea.WindowSizeMsg{Width: 120, Height: 40}))
	drain(model.(Model).reload())
	drain(step(tea.KeyPressMsg{Code: '1', Mod: tea.ModAlt}))

	m := model.(Model)
	if !m.focus || m.activeTerminal == nil {
		t.Fatalf("expected focus on the chatty executor, got focus=%v activeTerminal=%v", m.focus, m.activeTerminal)
	}

	// Let output ramp up to a steady, heavy rate before measuring.
	time.Sleep(300 * time.Millisecond)

	const n = 100
	var total, max time.Duration
	for i := 0; i < n; i++ {
		start := time.Now()
		_ = m.View()
		elapsed := time.Since(start)
		total += elapsed
		if elapsed > max {
			max = elapsed
		}
	}
	avg := total / n
	t.Logf("View() latency under heavy chatty output: avg=%v max=%v (n=%d)", avg, max, n)
	// Guards against the severe regression this reproduces, not a tight
	// perf budget — see the matching comment in
	// internal/terminal/session_test.go's TestSendKeyLatencyUnderHeavyOutput
	// for why 300ms (shared CI runners are noisy, and this test's own
	// chatty child already competes hard for the same CPU).
	if max > 300*time.Millisecond {
		t.Errorf("View() latency too high under heavy output: max=%v (want < 300ms) — this blocks the next keystroke from being processed", max)
	}
}
