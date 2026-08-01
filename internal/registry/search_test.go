package registry

import (
	"context"
	"strings"
	"testing"
)

func TestSearchPaginationIsStable(t *testing.T) {
	svc, projectID, root := newTestService(t)
	for _, name := range []string{"a.go", "b.go", "c.go", "d.go"} {
		writeSource(t, root, name, "package p\nfunc F(){}\n")
	}
	if _, err := svc.Scan(projectID); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	page1, total, err := svc.Search(projectID, SearchQuery{Limit: 2, Offset: 0})
	if err != nil {
		t.Fatalf("Search page1: %v", err)
	}
	page2, _, err := svc.Search(projectID, SearchQuery{Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("Search page2: %v", err)
	}
	if total != 4 {
		t.Fatalf("expected total=4, got %d", total)
	}
	if len(page1) != 2 || len(page2) != 2 {
		t.Fatalf("expected 2 results per page, got %d/%d", len(page1), len(page2))
	}
	seen := map[string]bool{}
	for _, r := range append(page1, page2...) {
		if seen[r.Entry.Path] {
			t.Fatalf("duplicate result across pages: %s", r.Entry.Path)
		}
		seen[r.Entry.Path] = true
	}
}

func TestSearchAliasExpansionFindsIndirectMatches(t *testing.T) {
	svc, projectID, root := newTestService(t)
	writeSource(t, root, "storage/store.go", "package storage\nfunc Store(){}\n")
	if _, err := svc.Scan(projectID); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	cfg, err := svc.LoadConfig(projectID)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cfg.SearchAliases = map[string][]string{"db": {"storage", "store"}}
	if err := svc.SaveConfig(projectID, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	results, total, err := svc.Search(projectID, SearchQuery{Text: "db"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if total != 1 || results[0].Entry.Path != "storage/store.go" {
		t.Fatalf("expected alias expansion to find storage/store.go, got total=%d results=%+v", total, results)
	}
}

type rrfEmbedder struct{}

func (rrfEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	if strings.Contains(text, "alpha") {
		return []float32{1, 0}, nil
	}
	return []float32{0, 1}, nil
}

func TestSearchRRFRanksDualSignalAboveSingleSignal(t *testing.T) {
	svc, projectID, root := newTestService(t)
	writeSource(t, root, "alpha.go", "package alpha\nfunc Alpha(){}\n")
	writeSource(t, root, "beta.go", "package beta\nfunc Beta(){}\n")
	if _, err := svc.Scan(projectID); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	svc.SetEmbedder(rrfEmbedder{})

	results, _, err := svc.Search(projectID, SearchQuery{Text: "alpha", Semantic: true})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 || results[0].Entry.Path != "alpha.go" {
		t.Fatalf("expected alpha.go (matches both lexically and semantically) ranked first, got %+v", results)
	}
}
