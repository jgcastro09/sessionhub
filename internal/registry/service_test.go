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
	svc := New(catalog, events.NewBus())
	t.Cleanup(func() { _ = svc.Close() })
	return svc, proj.ID, root
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

func symbolNames(symbols map[string][]SymbolRef, category string) []string {
	var out []string
	for _, s := range symbols[category] {
		out = append(out, s.Name)
	}
	return out
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
	if names := symbolNames(goEntry.Symbols, "functions"); len(names) != 1 || names[0] != "NewProject" {
		t.Fatalf("expected NewProject symbol, got %v", goEntry.Symbols)
	}
	if goEntry.ReviewStatus != ReviewNeedsReview {
		t.Fatalf("new entries must default to needs_review, got %q", goEntry.ReviewStatus)
	}
	tsEntry, ok := byPath["web/src/App.tsx"]
	if !ok || tsEntry.Language != "typescript" {
		t.Fatalf("unexpected typescript entry: %+v", tsEntry)
	}
	pyEntry, ok := byPath["scripts/build.py"]
	if !ok || pyEntry.Language != "python" {
		t.Fatalf("unexpected python entry: %+v", pyEntry)
	}
	if names := symbolNames(pyEntry.Symbols, "functions"); len(names) != 1 || names[0] != "build" {
		t.Fatalf("expected build symbol, got %v", pyEntry.Symbols)
	}
}

func TestHealthFlagsMissingCoverage(t *testing.T) {
	svc, projectID, root := newTestService(t)
	writeSource(t, root, "a.go", "package a\n")
	if _, err := svc.Scan(projectID); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	writeSource(t, root, "b.go", "package a\nfunc B() {}\n")

	report, err := svc.Health(projectID)
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if len(report.MissingPaths) != 1 || report.MissingPaths[0] != "b.go" {
		t.Fatalf("unexpected missing paths: %v", report.MissingPaths)
	}

	if _, err := svc.Scan(projectID); err != nil {
		t.Fatalf("second Scan: %v", err)
	}
	report, err = svc.Health(projectID)
	if err != nil {
		t.Fatalf("Health after rescan: %v", err)
	}
	if len(report.MissingPaths) != 0 {
		t.Fatalf("expected full coverage after rescan, got %+v", report.MissingPaths)
	}
	// Neither file has been reviewed yet, so the registry as a whole is
	// correctly still not Healthy — StaleReviews must list both.
	if len(report.StaleReviews) != 2 {
		t.Fatalf("expected both unreviewed entries flagged stale, got %v", report.StaleReviews)
	}
	if report.Healthy {
		t.Fatalf("registry with unreviewed entries must not report healthy")
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
	if len(renamed.PreviousPaths) != 1 || renamed.PreviousPaths[0] != "old/name.go" {
		t.Fatalf("expected previous_paths to record the old path, got %v", renamed.PreviousPaths)
	}
}

func TestSearchIsDeterministicAndFiltered(t *testing.T) {
	svc, projectID, root := newTestService(t)
	writeSource(t, root, "internal/tasks/service.go", "package tasks\nfunc Create() {}\n")
	writeSource(t, root, "internal/registry/service.go", "package registry\nfunc Scan() {}\n")
	if _, err := svc.Scan(projectID); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	results, total, err := svc.Search(projectID, SearchQuery{Text: "tasks", Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if total != 1 || len(results) != 1 || results[0].Entry.Path != "internal/tasks/service.go" {
		t.Fatalf("unexpected search results: total=%d results=%+v", total, results)
	}

	if _, err := svc.SemanticSearch(context.Background(), projectID, "tasks", 10); err != ErrSemanticUnavailable {
		t.Fatalf("expected ErrSemanticUnavailable, got %v", err)
	}
}

func TestSearchFiltersByCategory(t *testing.T) {
	svc, projectID, root := newTestService(t)
	writeSource(t, root, "a.go", "package a\n")
	writeSource(t, root, "b.py", "def b():\n    pass\n")
	if _, err := svc.Scan(projectID); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	results, total, err := svc.Search(projectID, SearchQuery{Categories: []string{"go"}})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if total != 1 || results[0].Entry.Path != "a.go" {
		t.Fatalf("expected only the go entry, got total=%d results=%+v", total, results)
	}
}

// fakeEmbedder maps known substrings to hand-picked vectors so tests can
// assert on ranking without a real embedding engine.
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
