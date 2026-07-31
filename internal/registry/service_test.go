package registry

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jgcastro09/sessionhub/internal/events"
	"github.com/jgcastro09/sessionhub/internal/project"
)

func newTestService(t *testing.T) (*Service, string, string) {
	t.Helper()
	root := t.TempDir()
	proj, err := project.Init(root, "Test Project")
	if err != nil {
		t.Fatalf("project.Init: %v", err)
	}
	catalog, err := project.OpenCatalog(t.TempDir())
	if err != nil {
		t.Fatalf("OpenCatalog: %v", err)
	}
	if _, err := catalog.Attach(root); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	return New(catalog, events.NewBus()), proj.ID, root
}

func writeSource(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func TestScanDiscoversMixedProject(t *testing.T) {
	svc, projectID, root := newTestService(t)
	writeSource(t, root, "internal/app/app.go", "package app\n\nfunc NewProject() {}\n")
	writeSource(t, root, "web/src/App.tsx", "export function App() { return null }\n")
	writeSource(t, root, "scripts/build.py", "def build():\n    pass\n")
	writeSource(t, root, "node_modules/dep/index.js", "module.exports = {}\n")

	entries, err := svc.Scan(projectID)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	byPath := map[string]Entry{}
	for _, e := range entries {
		byPath[e.Path] = e
	}
	if _, ok := byPath["node_modules/dep/index.js"]; ok {
		t.Fatalf("node_modules should be excluded by default")
	}
	goEntry, ok := byPath["internal/app/app.go"]
	if !ok {
		t.Fatalf("missing entry for internal/app/app.go")
	}
	if goEntry.Language != "go" || goEntry.Category != "go" {
		t.Fatalf("unexpected classification: %+v", goEntry)
	}
	if len(goEntry.Symbols) != 1 || goEntry.Symbols[0] != "NewProject" {
		t.Fatalf("expected NewProject symbol, got %v", goEntry.Symbols)
	}
	tsEntry, ok := byPath["web/src/App.tsx"]
	if !ok || tsEntry.Language != "typescript" {
		t.Fatalf("unexpected typescript entry: %+v", tsEntry)
	}
	pyEntry, ok := byPath["scripts/build.py"]
	if !ok || pyEntry.Language != "python" || len(pyEntry.Symbols) != 1 || pyEntry.Symbols[0] != "build" {
		t.Fatalf("unexpected python entry: %+v", pyEntry)
	}
}

func TestValidateFlagsMissingCoverage(t *testing.T) {
	svc, projectID, root := newTestService(t)
	writeSource(t, root, "a.go", "package a\n")
	if _, err := svc.Scan(projectID); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	writeSource(t, root, "b.go", "package a\nfunc B() {}\n")

	report, err := svc.Validate(projectID)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if report.OK() {
		t.Fatalf("expected coverage gap for b.go, got %+v", report)
	}
	if len(report.MissingPaths) != 1 || report.MissingPaths[0] != "b.go" {
		t.Fatalf("unexpected missing paths: %v", report.MissingPaths)
	}

	if _, err := svc.Scan(projectID); err != nil {
		t.Fatalf("second Scan: %v", err)
	}
	report, err = svc.Validate(projectID)
	if err != nil {
		t.Fatalf("Validate after rescan: %v", err)
	}
	if !report.OK() {
		t.Fatalf("expected full coverage after rescan, got %+v", report)
	}
}

func TestReviewIsPreservedAcrossRescan(t *testing.T) {
	svc, projectID, root := newTestService(t)
	writeSource(t, root, "internal/foo/foo.go", "package foo\nfunc Foo() {}\n")
	entries, err := svc.Scan(projectID)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	entryID := entries[0].EntryID

	if _, err := svc.Review(projectID, entryID, ReviewInput{
		Module: "foo", Description: "Handles foo.", Criticality: "high",
	}); err != nil {
		t.Fatalf("Review: %v", err)
	}

	// Content changes, but the review must survive a rescan.
	writeSource(t, root, "internal/foo/foo.go", "package foo\nfunc Foo() {}\nfunc Bar() {}\n")
	if _, err := svc.Scan(projectID); err != nil {
		t.Fatalf("rescan: %v", err)
	}
	updated, err := svc.Get(projectID, entryID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.Description != "Handles foo." || updated.Criticality != "high" {
		t.Fatalf("review fields lost after rescan: %+v", updated)
	}
	if len(updated.Symbols) != 2 {
		t.Fatalf("expected automatic fields to still update: %+v", updated)
	}
}

func TestRenameDetectionPreservesEntryID(t *testing.T) {
	svc, projectID, root := newTestService(t)
	writeSource(t, root, "old/name.go", "package old\nfunc Keep() {}\n")
	entries, err := svc.Scan(projectID)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	original := entries[0]

	if err := os.Rename(filepath.Join(root, "old", "name.go"), filepath.Join(root, "old", "renamed.go")); err != nil {
		t.Fatalf("rename: %v", err)
	}
	entries, err = svc.Scan(projectID)
	if err != nil {
		t.Fatalf("Scan after rename: %v", err)
	}
	var renamed *Entry
	for i := range entries {
		if entries[i].Path == "old/renamed.go" {
			renamed = &entries[i]
		}
	}
	if renamed == nil {
		t.Fatalf("renamed file not found in %+v", entries)
	}
	if renamed.EntryID != original.EntryID {
		t.Fatalf("rename did not preserve entry_id: got %s want %s", renamed.EntryID, original.EntryID)
	}
}

func TestSearchIsLexicalAndDeterministic(t *testing.T) {
	svc, projectID, root := newTestService(t)
	writeSource(t, root, "internal/tasks/service.go", "package tasks\nfunc Create() {}\n")
	writeSource(t, root, "internal/registry/service.go", "package registry\nfunc Scan() {}\n")
	if _, err := svc.Scan(projectID); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	results, err := svc.Search(projectID, "tasks", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 || results[0].Entry.Path != "internal/tasks/service.go" {
		t.Fatalf("unexpected search results: %+v", results)
	}

	if _, err := svc.SemanticSearch(context.Background(), projectID, "tasks", 10); err != ErrSemanticUnavailable {
		t.Fatalf("expected ErrSemanticUnavailable, got %v", err)
	}
}

// fakeEmbedder maps known substrings to hand-picked vectors so tests can
// assert on ranking without a real embedding engine. Anything else gets a
// deterministic hash-based vector so re-embedding is still exercised.
type fakeEmbedder struct{ calls int }

func (f *fakeEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	f.calls++
	switch {
	case strings.Contains(text, "tasks"):
		return []float32{1, 0}, nil
	case strings.Contains(text, "registry"):
		return []float32{0, 1}, nil
	default:
		return []float32{0.5, 0.5}, nil
	}
}

func TestSemanticSearchRanksByCosineSimilarityAndCachesByHash(t *testing.T) {
	svc, projectID, root := newTestService(t)
	writeSource(t, root, "internal/tasks/service.go", "package tasks\nfunc Create() {}\n")
	writeSource(t, root, "internal/registry/service.go", "package registry\nfunc Scan() {}\n")
	if _, err := svc.Scan(projectID); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	embedder := &fakeEmbedder{}
	svc.SetEmbedder(embedder)

	results, err := svc.SemanticSearch(context.Background(), projectID, "tasks", 10)
	if err != nil {
		t.Fatalf("SemanticSearch: %v", err)
	}
	if len(results) != 2 || results[0].Entry.Path != "internal/tasks/service.go" {
		t.Fatalf("expected the tasks entry ranked first, got %+v", results)
	}
	callsAfterFirst := embedder.calls

	if err := svc.EnsureSemanticIndex(context.Background(), projectID); err != nil {
		t.Fatalf("EnsureSemanticIndex: %v", err)
	}
	if embedder.calls != callsAfterFirst {
		t.Fatalf("expected no new embedding calls for unchanged entries, calls went from %d to %d", callsAfterFirst, embedder.calls)
	}
}

func TestEntryExists(t *testing.T) {
	svc, projectID, root := newTestService(t)
	writeSource(t, root, "a.go", "package a\n")
	entries, err := svc.Scan(projectID)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	exists, err := svc.EntryExists(projectID, entries[0].EntryID)
	if err != nil || !exists {
		t.Fatalf("expected entry to exist: %v %v", exists, err)
	}
	exists, err = svc.EntryExists(projectID, "entry_doesnotexist")
	if err != nil || exists {
		t.Fatalf("expected entry to not exist: %v %v", exists, err)
	}
}
