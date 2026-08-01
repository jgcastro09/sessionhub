package registry

import (
	"encoding/json"
	"testing"
	"time"
)

func TestEntryJSONRoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	original := Entry{
		EntryID: "entry_abc", Path: "internal/foo/foo.go", Filename: "foo.go", Extension: ".go",
		Language: "go", Kind: KindImplementation,
		Category: "go", Module: "Backend", Area: "backend", Role: "service",
		Symbols:      map[string][]SymbolRef{"functions": {{Name: "Foo", Line: 3}}},
		Includes:     []string{"a"},
		Imports:      []string{"b"},
		Exports:      []string{"c"},
		Dependencies: []string{"a", "b"},
		LineCount:    10, SizeBytes: 200, LastModifiedAt: now,
		Hash: "deadbeef", Status: StatusActive,
		Description: "does foo things", Responsibilities: []string{"foo"},
		Criticality: CriticalityImportant, RelatedFiles: []string{"entry_def"},
		ProbableRelatedFiles: []string{"entry_ghi"}, PreviousPaths: []string{"old/foo.go"},
		LastReviewedHash: "deadbeef", ReviewStatus: ReviewReviewed, ReviewedAt: &now, ReviewedBy: "agent",
		LastScannedAt: now, CreatedAt: now, UpdatedAt: now,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded Entry
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.EntryID != original.EntryID || decoded.Path != original.Path {
		t.Fatalf("basic identity fields did not round-trip: %+v", decoded)
	}
	if len(decoded.Symbols["functions"]) != 1 || decoded.Symbols["functions"][0] != (SymbolRef{Name: "Foo", Line: 3}) {
		t.Fatalf("symbols did not round-trip: %+v", decoded.Symbols)
	}
	if decoded.ReviewStatus != ReviewReviewed || decoded.LastReviewedHash != "deadbeef" {
		t.Fatalf("review state did not round-trip: %+v", decoded)
	}
	if decoded.Criticality != CriticalityImportant {
		t.Fatalf("criticality did not round-trip: %+v", decoded)
	}
	if decoded.ReviewedAt == nil || !decoded.ReviewedAt.Equal(*original.ReviewedAt) {
		t.Fatalf("reviewed_at did not round-trip: %+v", decoded.ReviewedAt)
	}
}

func TestEntryStale(t *testing.T) {
	e := Entry{Hash: "h1", LastReviewedHash: "h1", ReviewStatus: ReviewReviewed}
	if e.Stale() {
		t.Fatalf("expected a freshly reviewed entry to not be stale")
	}
	e.Hash = "h2"
	if !e.Stale() {
		t.Fatalf("expected a hash mismatch to make the entry stale")
	}
	e.Hash = "h1"
	e.ReviewStatus = ReviewNeedsReview
	if !e.Stale() {
		t.Fatalf("expected needs_review status to make the entry stale even with matching hashes")
	}
}
