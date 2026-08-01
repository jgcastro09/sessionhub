package registry

import "testing"

func newActiveEntry(id, path string, kind Kind) Entry {
	return Entry{EntryID: id, Path: path, Filename: path, Kind: kind, Status: StatusActive, ReviewStatus: ReviewNeedsReview}
}

func TestBuildGraphImplementsEdgeForHeaderImplPair(t *testing.T) {
	header := newActiveEntry("e_header", "lib/foo.h", KindHeader)
	impl := newActiveEntry("e_impl", "lib/foo.cpp", KindImplementation)
	g, _ := BuildGraph([]Entry{header, impl})

	found := false
	for _, e := range g.Edges {
		if e.Kind == "implements" && e.From == "e_header" && e.To == "e_impl" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an implements edge between foo.h and foo.cpp, got %+v", g.Edges)
	}
}

func TestBuildGraphResolvesUnambiguousImport(t *testing.T) {
	a := newActiveEntry("e_a", "internal/foo/a.go", KindImplementation)
	a.Imports = []string{"internal/foo/b"}
	b := newActiveEntry("e_b", "internal/foo/b.go", KindImplementation)
	g, issues := BuildGraph([]Entry{a, b})

	found := false
	for _, e := range g.Edges {
		if e.Kind == "imports" && e.From == "e_a" && e.To == "e_b" && e.Confidence == "resolved" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a resolved imports edge, got edges=%+v issues=%+v", g.Edges, issues)
	}
}

func TestBuildGraphDropsAmbiguousImportRatherThanGuessing(t *testing.T) {
	a := newActiveEntry("e_a", "internal/a.go", KindImplementation)
	a.Imports = []string{"shared"}
	b1 := newActiveEntry("e_b1", "internal/foo/shared.go", KindImplementation)
	b2 := newActiveEntry("e_b2", "internal/bar/shared.go", KindImplementation)
	g, issues := BuildGraph([]Entry{a, b1, b2})

	for _, e := range g.Edges {
		if e.Kind == "imports" {
			t.Fatalf("expected the ambiguous import to be dropped, not guessed, got edge %+v", e)
		}
	}
	if len(issues) != 1 || issues[0].Reason != "ambiguous" {
		t.Fatalf("expected exactly one ambiguous dependency issue, got %+v", issues)
	}
}

func TestImpactAnalysisRespectsNodeLimitAndPrioritizesNeedsReview(t *testing.T) {
	seed := newActiveEntry("seed", "a.go", KindImplementation)
	var g Graph
	g.Nodes = append(g.Nodes, GraphNode{ID: "seed", Kind: "file"})
	for i := 0; i < 10; i++ {
		id := "n" + string(rune('a'+i))
		status := ReviewReviewed
		if i%3 == 0 {
			status = ReviewNeedsReview
		}
		g.Nodes = append(g.Nodes, GraphNode{ID: id, Kind: "file", ReviewStatus: status})
		g.Edges = append(g.Edges, GraphEdge{From: "seed", To: id, Kind: "related", Confidence: "confirmed"})
	}
	_ = seed

	sub := ImpactAnalysis(g, "seed", 1, 4)
	if len(sub.Nodes) > 4 {
		t.Fatalf("expected at most 4 nodes, got %d", len(sub.Nodes))
	}
	seedPresent := false
	needsReviewCount := 0
	for _, n := range sub.Nodes {
		if n.ID == "seed" {
			seedPresent = true
		}
		if n.ReviewStatus == ReviewNeedsReview {
			needsReviewCount++
		}
	}
	if !seedPresent {
		t.Fatalf("expected the seed node to always be included")
	}
	if needsReviewCount == 0 {
		t.Fatalf("expected needs_review nodes to be prioritized when truncating, got %+v", sub.Nodes)
	}
}
