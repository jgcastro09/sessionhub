package executor

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/jgcastro09/sessionhub/internal/domain"
	"github.com/jgcastro09/sessionhub/internal/store"
	"github.com/jgcastro09/sessionhub/internal/terminal"
)

// TestHandleEventExitCodeDoesNotDeadlock guards against a regression where
// handleEvent took s.mu.Lock() and then, only for EventState events carrying
// a non-nil ExitCode (i.e. whenever any executor process exits), re-acquired
// s.mu.RLock() while still holding the write lock. sync.RWMutex isn't
// reentrant, so that self-deadlocked the goroutine permanently — freezing
// Service.Run()'s single event loop while still holding the same lock
// Start() needs at its end to register any future instance. In practice:
// the first terminal in a session would start fine, but the moment any
// process anywhere exited, starting a second terminal would hang forever.
func TestHandleEventExitCodeDoesNotDeadlock(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "svc.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	terminals := terminal.NewManager(ctx, nil, 100)
	svc := New(ctx, st, terminals)

	const instanceID = "inst-exit-test"
	svc.instances[instanceID] = domain.Instance{ID: instanceID, SessionID: "s1", ExecutorID: "e1"}
	svc.configs[instanceID] = domain.ExecutorConfig{ID: "e1"}

	exitCode := 1
	done := make(chan struct{})
	go func() {
		svc.handleEvent(terminal.Event{
			InstanceID: instanceID,
			Kind:       terminal.EventState,
			State:      domain.StateError,
			ExitCode:   &exitCode,
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("handleEvent deadlocked on an EventState event with a non-nil ExitCode")
	}
}

func TestRulelessAutomationCompletesAfterSettledOutput(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "svc.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := New(ctx, st, terminal.NewManager(ctx, nil, 100))
	instanceID := "inst-automation-idle"
	done := make(chan WorkResult, 1)
	svc.instances[instanceID] = domain.Instance{ID: instanceID, SessionID: "s1", ExecutorID: "e1"}
	svc.configs[instanceID] = domain.ExecutorConfig{ID: "e1"}
	svc.work[instanceID] = &activeWork{
		ID:         "work-1",
		InstanceID: instanceID,
		Prompt:     "build it",
		Output:     []byte("the response is complete"),
		StartedAt:  time.Now().Add(-10 * time.Second),
		LastOutput: time.Now().Add(-automationCompletionQuiet - time.Second),
		done:       done,
	}

	svc.checkWork(time.Now())
	select {
	case result := <-done:
		if result.Outcome != domain.StateSucceeded || result.Reason != "executor output settled after automation response" {
			t.Fatalf("unexpected idle result: %#v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("ruleless automation did not complete after settled output")
	}
}

func TestAutomationCompletionTokenFinishesWorkImmediately(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "svc.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := New(ctx, st, terminal.NewManager(ctx, nil, 100))
	instanceID := "inst-automation-token"
	done := make(chan WorkResult, 1)
	svc.instances[instanceID] = domain.Instance{ID: instanceID, SessionID: "s1", ExecutorID: "e1"}
	svc.configs[instanceID] = domain.ExecutorConfig{ID: "e1"}
	svc.work[instanceID] = &activeWork{
		ID: "work-token", InstanceID: instanceID, Prompt: "build it",
		CompletionToken: AutomationCompletionToken, StartedAt: time.Now(), LastOutput: time.Now(), done: done,
	}

	svc.handleOutput(instanceID, []byte("Task complete\n"+AutomationCompletionToken))
	select {
	case result := <-done:
		if result.Outcome != domain.StateSucceeded || result.Reason != "executor confirmed automation completion" {
			t.Fatalf("unexpected completion result: %#v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("automation completion token did not resolve the work")
	}
}
