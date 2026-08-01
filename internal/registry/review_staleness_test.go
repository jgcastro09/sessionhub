package registry

import "testing"

// TestReviewedEntryFlaggedNeedsReviewAfterContentChange is the regression
// guard for the core bug this rewrite fixes: applyDiscovery used to
// unconditionally overwrite Hash while never resetting Reviewed, so an
// approved file could be edited and silently stay "reviewed" forever.
func TestReviewedEntryFlaggedNeedsReviewAfterContentChange(t *testing.T) {
	svc, projectID, root := newTestService(t)
	writeSource(t, root, "internal/foo/foo.go", "package foo\nfunc Foo() {}\n")
	entries, err := svc.Scan(projectID)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	entryID := entries[0].EntryID

	reviewed, err := svc.Review(projectID, entryID, ReviewInput{
		Description: "Handles foo.", Criticality: CriticalityImportant, Responsibilities: []string{"does foo things"},
	})
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if reviewed.ReviewStatus != ReviewReviewed || reviewed.LastReviewedHash != reviewed.Hash {
		t.Fatalf("expected entry to be marked reviewed against its current hash: %+v", reviewed)
	}
	hashAtReview := reviewed.Hash

	// Content changes — the review must NOT survive this.
	writeSource(t, root, "internal/foo/foo.go", "package foo\nfunc Foo() {}\nfunc Bar() {}\n")
	if _, err := svc.Scan(projectID); err != nil {
		t.Fatalf("rescan: %v", err)
	}
	updated, err := svc.Get(projectID, entryID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.ReviewStatus != ReviewNeedsReview {
		t.Fatalf("expected ReviewStatus to flip to needs_review after content change, got %q", updated.ReviewStatus)
	}
	if updated.Hash == hashAtReview {
		t.Fatalf("expected hash to change after content edit")
	}
	if updated.LastReviewedHash != hashAtReview {
		t.Fatalf("expected LastReviewedHash to keep pointing at the reviewed hash, got %q want %q", updated.LastReviewedHash, hashAtReview)
	}
	if !updated.Stale() {
		t.Fatalf("expected Entry.Stale() to report true once hash and last_reviewed_hash diverge")
	}
	// Human-owned fields must survive — only the review *status* invalidates.
	if updated.Description != "Handles foo." || updated.Criticality != CriticalityImportant {
		t.Fatalf("review fields were lost, not just invalidated: %+v", updated)
	}
	if names := symbolNames(updated.Symbols, "functions"); len(names) != 2 {
		t.Fatalf("expected automatic fields (symbols) to still update: %+v", updated.Symbols)
	}
}

func TestReReviewAfterStalenessClearsFlag(t *testing.T) {
	svc, projectID, root := newTestService(t)
	writeSource(t, root, "a.go", "package a\nfunc A() {}\n")
	entries, err := svc.Scan(projectID)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	entryID := entries[0].EntryID
	if _, err := svc.Review(projectID, entryID, ReviewInput{Description: "d", Criticality: CriticalityStandard}); err != nil {
		t.Fatalf("Review: %v", err)
	}
	writeSource(t, root, "a.go", "package a\nfunc A() {}\nfunc B() {}\n")
	if _, err := svc.Scan(projectID); err != nil {
		t.Fatalf("rescan: %v", err)
	}
	stale, err := svc.Get(projectID, entryID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if stale.ReviewStatus != ReviewNeedsReview {
		t.Fatalf("expected needs_review before re-review, got %q", stale.ReviewStatus)
	}

	reReviewed, err := svc.Review(projectID, entryID, ReviewInput{Description: "d2", Criticality: CriticalityStandard})
	if err != nil {
		t.Fatalf("re-Review: %v", err)
	}
	if reReviewed.ReviewStatus != ReviewReviewed || reReviewed.Hash != reReviewed.LastReviewedHash {
		t.Fatalf("expected re-review to clear staleness: %+v", reReviewed)
	}
	if reReviewed.Stale() {
		t.Fatalf("expected Entry.Stale() to report false immediately after review")
	}
}

// TestUnreviewedEntryStaysNeedsReviewAcrossRescans is a regression guard
// against reintroducing the old inverted check (Validate used to skip
// exactly the entries that most needed staleness detection).
func TestUnreviewedEntryStaysNeedsReviewAcrossRescans(t *testing.T) {
	svc, projectID, root := newTestService(t)
	writeSource(t, root, "a.go", "package a\n")
	if _, err := svc.Scan(projectID); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if _, err := svc.Scan(projectID); err != nil {
		t.Fatalf("second Scan: %v", err)
	}
	entries, err := svc.List(projectID, false)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 1 || entries[0].ReviewStatus != ReviewNeedsReview {
		t.Fatalf("expected the never-reviewed entry to stay needs_review, got %+v", entries)
	}
}

func TestReviewRejectsUnknownRelatedFileID(t *testing.T) {
	svc, projectID, root := newTestService(t)
	writeSource(t, root, "a.go", "package a\n")
	entries, err := svc.Scan(projectID)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	_, err = svc.Review(projectID, entries[0].EntryID, ReviewInput{RelatedFiles: []string{"entry_doesnotexist"}})
	if err == nil {
		t.Fatalf("expected Review to reject an unknown related-file entry_id")
	}
}

func TestReviewRejectsSelfRelation(t *testing.T) {
	svc, projectID, root := newTestService(t)
	writeSource(t, root, "a.go", "package a\n")
	entries, err := svc.Scan(projectID)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	_, err = svc.Review(projectID, entries[0].EntryID, ReviewInput{RelatedFiles: []string{entries[0].EntryID}})
	if err == nil {
		t.Fatalf("expected Review to reject an entry relating to itself")
	}
}

func TestProbableRelatedFilesGroupsBySameStem(t *testing.T) {
	svc, projectID, root := newTestService(t)
	writeSource(t, root, "internal/foo/foo.go", "package foo\nfunc Foo() {}\n")
	writeSource(t, root, "internal/foo/foo_test.go", "package foo\nfunc TestFoo() {}\n")
	writeSource(t, root, "web/src/Button.tsx", "export function Button() { return null }\n")
	writeSource(t, root, "web/src/Button.test.tsx", "export function T() { return null }\n")
	entries, err := svc.Scan(projectID)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	byPath := map[string]Entry{}
	for _, e := range entries {
		byPath[e.Path] = e
	}
	foo := byPath["internal/foo/foo.go"]
	fooTest := byPath["internal/foo/foo_test.go"]
	if len(foo.ProbableRelatedFiles) != 1 || foo.ProbableRelatedFiles[0] != fooTest.EntryID {
		t.Fatalf("expected foo.go <-> foo_test.go grouping, got %+v", foo.ProbableRelatedFiles)
	}
	button := byPath["web/src/Button.tsx"]
	buttonTest := byPath["web/src/Button.test.tsx"]
	if len(button.ProbableRelatedFiles) != 1 || button.ProbableRelatedFiles[0] != buttonTest.EntryID {
		t.Fatalf("expected Button.tsx <-> Button.test.tsx grouping, got %+v", button.ProbableRelatedFiles)
	}
}
