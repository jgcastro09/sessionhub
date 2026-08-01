package registry

import "testing"

func TestEnsureFreshNoOpWhenAlreadyFresh(t *testing.T) {
	svc, projectID, root := newTestService(t)
	writeSource(t, root, "a.go", "package a\n")
	if _, err := svc.Scan(projectID); err != nil {
		t.Fatalf("Scan: %v", err)
	}

	result, err := svc.EnsureFresh(projectID)
	if err != nil {
		t.Fatalf("EnsureFresh: %v", err)
	}
	if result.Reconciled {
		t.Fatalf("expected no reconciliation when nothing drifted, got %+v", result)
	}
	if result.Freshness.Status != FreshnessFresh {
		t.Fatalf("expected freshness fresh, got %+v", result.Freshness)
	}

	freshness, err := svc.GetFreshness(projectID)
	if err != nil {
		t.Fatalf("GetFreshness: %v", err)
	}
	if freshness.Status != FreshnessFresh {
		t.Fatalf("expected persisted freshness fresh, got %+v", freshness)
	}
}

// TestEnsureFreshReconcilesUnscannedEdit is the core Fase 1.5 behavior: a
// context-delivering caller (search, Reader, impact analysis) must never
// see stale results just because nobody remembered to call Scan() first.
func TestEnsureFreshReconcilesUnscannedEdit(t *testing.T) {
	svc, projectID, root := newTestService(t)
	writeSource(t, root, "a.go", "package a\nfunc A(){}\n")
	if _, err := svc.Scan(projectID); err != nil {
		t.Fatalf("Scan: %v", err)
	}

	// Edit on disk, no Scan() call — EnsureFresh must catch and fix this.
	writeSource(t, root, "a.go", "package a\nfunc A(){}\nfunc B(){}\n")

	result, err := svc.EnsureFresh(projectID)
	if err != nil {
		t.Fatalf("EnsureFresh: %v", err)
	}
	if !result.Reconciled {
		t.Fatalf("expected EnsureFresh to reconcile the unscanned edit, got %+v", result)
	}
	if len(result.Health.PendingRescan) != 0 || len(result.Health.MissingPaths) != 0 {
		t.Fatalf("expected the disk-vs-registry drift to be gone after reconciliation, got %+v", result.Health)
	}
	if result.Freshness.Status != FreshnessFresh {
		t.Fatalf("expected freshness fresh after reconciliation, got %+v", result.Freshness)
	}

	entry, err := svc.Get(projectID, mustFindEntryID(t, svc, projectID, "a.go"))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(entry.Symbols["functions"]) != 2 {
		t.Fatalf("expected the reconciled entry to reflect the edit (2 functions), got %+v", entry.Symbols)
	}
}

func TestEnsureFreshNewPendingFileMarksUnhealthy(t *testing.T) {
	svc, projectID, root := newTestService(t)
	writeSource(t, root, "a.go", "package a\n")
	if _, err := svc.Scan(projectID); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	writeSource(t, root, "notes.unknownext", "some text\n")

	result, err := svc.EnsureFresh(projectID)
	if err != nil {
		t.Fatalf("EnsureFresh: %v", err)
	}
	if result.Health.PendingClassificationCount != 1 {
		t.Fatalf("expected 1 pending classification file, got %+v", result.Health)
	}
}

func mustFindEntryID(t *testing.T, svc *Service, projectID, path string) string {
	t.Helper()
	entries, err := svc.List(projectID, false)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, e := range entries {
		if e.Path == path {
			return e.EntryID
		}
	}
	t.Fatalf("entry for %s not found", path)
	return ""
}
