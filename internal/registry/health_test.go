package registry

import "testing"

func TestHealthFlagsStaleReviewAndDanglingRelationship(t *testing.T) {
	svc, projectID, root := newTestService(t)
	writeSource(t, root, "a.go", "package a\nfunc A(){}\n")
	writeSource(t, root, "b.go", "package a\nfunc B(){}\n")
	entries, err := svc.Scan(projectID)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	var aID, bID string
	for _, e := range entries {
		switch e.Path {
		case "a.go":
			aID = e.EntryID
		case "b.go":
			bID = e.EntryID
		}
	}
	if _, err := svc.Review(projectID, aID, ReviewInput{
		Description: "a", Criticality: CriticalityStandard, Responsibilities: []string{"r"},
		RelatedFiles: []string{bID},
	}); err != nil {
		t.Fatalf("Review a: %v", err)
	}
	if _, err := svc.Review(projectID, bID, ReviewInput{
		Description: "b", Criticality: CriticalityStandard, Responsibilities: []string{"r"},
	}); err != nil {
		t.Fatalf("Review b: %v", err)
	}

	report, err := svc.Health(projectID)
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if !report.Healthy {
		t.Fatalf("expected a fully reviewed, fully covered project to be healthy, got %+v", report)
	}

	// Edit a.go's content without re-reviewing — must flip Healthy false.
	writeSource(t, root, "a.go", "package a\nfunc A(){}\nfunc A2(){}\n")
	if _, err := svc.Scan(projectID); err != nil {
		t.Fatalf("rescan: %v", err)
	}
	report, err = svc.Health(projectID)
	if err != nil {
		t.Fatalf("Health after edit: %v", err)
	}
	if report.Healthy {
		t.Fatalf("expected staleness to block healthy status")
	}
	if len(report.StaleReviews) != 1 || report.StaleReviews[0] != aID {
		t.Fatalf("expected a.go to be flagged stale, got %v", report.StaleReviews)
	}
}

// TestHealthFlagsDanglingRelationshipWhenTargetRemoved exercises health.go's
// independent re-derivation directly: Service.Review already rejects an
// unknown related-file entry_id, so the only way a dangling reference can
// exist is external tampering with records/*.json (or a future code path
// that bypasses Review) — Health must still catch it.
func TestHealthFlagsDanglingRelationshipWhenTargetRemoved(t *testing.T) {
	svc, projectID, root := newTestService(t)
	writeSource(t, root, "a.go", "package a\n")
	entries, err := svc.Scan(projectID)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	entries[0].RelatedFiles = []string{"entry_doesnotexist"}
	if err := saveAllEntries(root, entries); err != nil {
		t.Fatalf("saveAllEntries: %v", err)
	}

	report, err := svc.Health(projectID)
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if len(report.DanglingRelationships) != 1 || report.DanglingRelationships[0] != entries[0].EntryID {
		t.Fatalf("expected the dangling relationship to be flagged, got %v", report.DanglingRelationships)
	}
	if report.Healthy {
		t.Fatalf("expected a dangling relationship to block healthy status")
	}
}

// TestHealthDetectsEditWithoutRescan is the regression test for the Fase 0
// bug where Health() only checked path existence against result.Files,
// never comparing the fresh discovery's hash/size/symbols/dependencies
// against what was stored — so an on-disk edit made without ever calling
// Scan() again was invisible to Health(), directly contradicting "verdade
// antes de conveniência."
func TestHealthDetectsEditWithoutRescan(t *testing.T) {
	svc, projectID, root := newTestService(t)
	writeSource(t, root, "a.go", "package a\nfunc A(){}\n")
	entries, err := svc.Scan(projectID)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if _, err := svc.Review(projectID, entries[0].EntryID, ReviewInput{
		Description: "a", Criticality: CriticalityStandard, Responsibilities: []string{"r"},
	}); err != nil {
		t.Fatalf("Review: %v", err)
	}
	report, err := svc.Health(projectID)
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if !report.Healthy {
		t.Fatalf("expected a freshly scanned project to be healthy, got %+v", report)
	}

	// Edit the file on disk directly — no Scan() call in between.
	writeSource(t, root, "a.go", "package a\nfunc A(){}\nfunc B(){}\n")

	report, err = svc.Health(projectID)
	if err != nil {
		t.Fatalf("Health after unscanned edit: %v", err)
	}
	if report.Healthy {
		t.Fatalf("expected an unscanned edit to be caught by Health() without requiring a prior Scan()")
	}
	if len(report.PendingRescan) != 1 || report.PendingRescan[0] != "a.go" {
		t.Fatalf("expected a.go to be flagged pending rescan, got %v", report.PendingRescan)
	}
	if len(report.StalenessReasons) == 0 {
		t.Fatalf("expected a staleness reason to be reported")
	}
}

// TestHealthPendingClassificationCountReflectsUnscannedFile is the
// regression test for the Fase 0 bug where PendingClassificationCount was
// read from the persisted pending.json (last written by Scan()) instead of
// the scan Health() itself just ran — so a new unclassified file created
// after the last Scan() was invisible to Health() until someone rescanned.
func TestHealthPendingClassificationCountReflectsUnscannedFile(t *testing.T) {
	svc, projectID, root := newTestService(t)
	writeSource(t, root, "a.go", "package a\n")
	if _, err := svc.Scan(projectID); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	report, err := svc.Health(projectID)
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if report.PendingClassificationCount != 0 {
		t.Fatalf("expected no pending files yet, got %d", report.PendingClassificationCount)
	}

	// A text file with an unrecognized extension, never scanned.
	writeSource(t, root, "notes.unknownext", "some free-form text content\n")

	report, err = svc.Health(projectID)
	if err != nil {
		t.Fatalf("Health after unscanned pending file: %v", err)
	}
	if report.PendingClassificationCount != 1 {
		t.Fatalf("expected the unscanned pending file to be counted immediately, got %d", report.PendingClassificationCount)
	}
	if report.Healthy {
		t.Fatalf("expected a pending file to block healthy status")
	}
}
