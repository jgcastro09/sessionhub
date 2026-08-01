package registry

import (
	"testing"
	"time"
)

func TestStartWatcherNoOpWhenDisabled(t *testing.T) {
	svc, projectID, _ := newTestService(t)
	w, err := svc.StartWatcher(projectID)
	if err != nil {
		t.Fatalf("StartWatcher: %v", err)
	}
	if w != nil {
		t.Fatalf("expected a nil Watcher when Config.Watch.Enabled is false (the default), got %+v", w)
	}
}

// TestWatcherReconcilesFileChange is the core Fase 1.5 watcher behavior: a
// file created/edited under an eligible root is picked up and scanned
// without anyone calling Scan() or EnsureFresh() explicitly.
func TestWatcherReconcilesFileChange(t *testing.T) {
	svc, projectID, root := newTestService(t)
	cfg, err := svc.LoadConfig(projectID)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cfg.Watch = WatchPolicy{Enabled: true, DebounceMS: 30}
	if err := svc.SaveConfig(projectID, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	w, err := svc.StartWatcher(projectID)
	if err != nil {
		t.Fatalf("StartWatcher: %v", err)
	}
	if w == nil {
		t.Fatalf("expected a non-nil Watcher once enabled")
	}
	t.Cleanup(func() { _ = w.Close() })

	reconciled := make(chan struct{}, 8)
	w.onReconcile = func(entries []Entry, err error) {
		if err != nil {
			t.Errorf("watcher reconcile: %v", err)
		}
		select {
		case reconciled <- struct{}{}:
		default:
		}
	}

	writeSource(t, root, "a.go", "package a\nfunc A(){}\n")

	select {
	case <-reconciled:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for the watcher to reconcile the new file")
	}

	entries, err := svc.List(projectID, false)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	found := false
	for _, e := range entries {
		if e.Path == "a.go" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a.go to have been scanned in by the watcher, got %+v", entries)
	}

	freshness, err := svc.GetFreshness(projectID)
	if err != nil {
		t.Fatalf("GetFreshness: %v", err)
	}
	if freshness.Status != FreshnessFresh {
		t.Fatalf("expected freshness fresh after reconciliation, got %+v", freshness)
	}
}

// TestWatcherCoalescesBurstIntoSingleScan verifies the debounce actually
// debounces: several rapid writes to the same file must cost one scan, not
// one per write.
func TestWatcherCoalescesBurstIntoSingleScan(t *testing.T) {
	svc, projectID, root := newTestService(t)
	cfg, err := svc.LoadConfig(projectID)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cfg.Watch = WatchPolicy{Enabled: true, DebounceMS: 200}
	if err := svc.SaveConfig(projectID, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	w, err := svc.StartWatcher(projectID)
	if err != nil {
		t.Fatalf("StartWatcher: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	var reconcileCount int
	reconciled := make(chan struct{}, 32)
	w.onReconcile = func(entries []Entry, err error) {
		reconcileCount++
		reconciled <- struct{}{}
	}

	for i := 0; i < 5; i++ {
		writeSource(t, root, "a.go", "package a\nfunc A(){}\n")
		time.Sleep(20 * time.Millisecond) // well under the 200ms debounce
	}

	select {
	case <-reconciled:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for the debounced reconcile")
	}
	// Give a generous margin for any spurious extra fire to show up before
	// asserting there was exactly one.
	time.Sleep(300 * time.Millisecond)
	if reconcileCount != 1 {
		t.Fatalf("expected exactly one debounced scan for a burst of 5 writes, got %d", reconcileCount)
	}
}
