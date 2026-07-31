package ui

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/jgcastro09/sessionhub/internal/app"
	"github.com/jgcastro09/sessionhub/internal/config"
	"github.com/jgcastro09/sessionhub/internal/domain"
)

// TestAltDigitSwitchesBetweenTwoTabs reproduces the reported flow end to
// end: alt+1 starts the first Executor, alt+2 must start and focus the
// second one, and pressing alt+1/alt+2 again must reattach to the running
// instances instead of spawning new ones.
func TestAltDigitSwitchesBetweenTwoTabs(t *testing.T) {
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

	command, args := "sleep", []string{"60"}
	if runtime.GOOS == "windows" {
		command, args = "cmd.exe", []string{"/d", "/c", "ping -n 60 127.0.0.1 >NUL"}
	}
	execA := domain.ExecutorConfig{ID: "exec_a", Name: "Alpha", Command: command, Args: args}
	execB := domain.ExecutorConfig{ID: "exec_b", Name: "Beta", Command: command, Args: args}
	for _, cfg := range []domain.ExecutorConfig{execA, execB} {
		if err := application.Store.SaveExecutor(ctx, cfg); err != nil {
			t.Fatal(err)
		}
	}
	project := domain.Project{Name: "teste", Root: root}
	project.SetExecutorIDs([]string{"exec_a", "exec_b"})
	project, err = application.Store.SaveProject(ctx, project)
	if err != nil {
		t.Fatal(err)
	}

	var model tea.Model = New(application)
	step := func(msg tea.Msg) tea.Cmd {
		var cmd tea.Cmd
		model, cmd = model.(Model).Update(msg)
		return cmd
	}
	// drain runs a cmd like the Bubble Tea runtime would, feeding resulting
	// messages back into Update (ignoring repeating ticks).
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
		case tickMsg, tea.BatchMsg:
			if batch, ok := msg.(tea.BatchMsg); ok {
				for _, c := range batch {
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
			}
			return
		default:
			drain(step(msg))
		}
	}

	drain(step(tea.WindowSizeMsg{Width: 120, Height: 40}))
	// Initial data load, like Init would trigger.
	drain(model.(Model).reload())

	if got := model.(Model).activeProject; got < 0 {
		t.Fatalf("expected an active project after reload, got %d", got)
	}

	altDigit := func(d rune) tea.KeyPressMsg {
		return tea.KeyPressMsg{Code: d, Mod: tea.ModAlt}
	}

	// alt+1 starts and focuses the first Executor.
	drain(step(altDigit('1')))
	m := model.(Model)
	if !m.focus || m.activeInstance.ExecutorID != "exec_a" {
		t.Fatalf("after alt+1: focus=%v executor=%q, want focus on exec_a", m.focus, m.activeInstance.ExecutorID)
	}
	firstInstance := m.activeInstance.ID

	// alt+2 while focused on tab 1 must start and focus the second Executor.
	drain(step(altDigit('2')))
	m = model.(Model)
	if !m.focus || m.activeInstance.ExecutorID != "exec_b" {
		t.Fatalf("after alt+2: focus=%v executor=%q, want focus on exec_b", m.focus, m.activeInstance.ExecutorID)
	}
	secondInstance := m.activeInstance.ID

	// alt+1 again must reattach to the original instance, not start a new one.
	drain(step(altDigit('1')))
	m = model.(Model)
	if m.activeInstance.ID != firstInstance {
		t.Fatalf("after alt+1 again: attached to %q, want original %q", m.activeInstance.ID, firstInstance)
	}
	// alt+2 again must reattach as well.
	drain(step(altDigit('2')))
	m = model.(Model)
	if m.activeInstance.ID != secondInstance {
		t.Fatalf("after alt+2 again: attached to %q, want original %q", m.activeInstance.ID, secondInstance)
	}

	if n := len(application.Terminals.List()); n != 2 {
		t.Fatalf("expected exactly 2 live terminals, got %d", n)
	}
}

// TestRenderTabsMarksAutomationActivatedTerminalOnline verifies the UI state
// that the Automation scheduler needs: it starts a lazy PTY directly through
// executor.Service, so no startedMsg ever reaches this Bubble Tea model. The
// corresponding topbar dot must nevertheless become filled immediately.
func TestRenderTabsMarksAutomationActivatedTerminalOnline(t *testing.T) {
	root := t.TempDir()
	paths := config.Paths{
		Root: root, Database: filepath.Join(root, "test.db"),
		Logs: filepath.Join(root, "logs"), Downloads: filepath.Join(root, "updates"),
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

	command, args := "sleep", []string{"60"}
	if runtime.GOOS == "windows" {
		command, args = "cmd.exe", []string{"/d", "/c", "ping -n 60 127.0.0.1 >NUL"}
	}
	cfg := domain.ExecutorConfig{ID: "exec_auto", Name: "Automation CLI", Command: command, Args: args}
	if err := application.Store.SaveExecutor(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	project := domain.Project{Name: "automation", Root: root}
	project.SetExecutorIDs([]string{cfg.ID})
	project, err = application.Store.SaveProject(ctx, project)
	if err != nil {
		t.Fatal(err)
	}

	model := New(application)
	model.width, model.height = 120, 40
	model.projects, model.executors, model.activeProject = []domain.Project{project}, []domain.ExecutorConfig{cfg}, 0
	if _, _, err := application.Executors.Start(ctx, project.ID, cfg.ID, 80, 24); err != nil {
		t.Fatal(err)
	}
	if got := model.renderTabs(); !strings.Contains(got, "● Automation CLI") {
		t.Fatalf("automation-started terminal should show an online dot, got %q", got)
	}
}
