package registry

import "testing"

func TestClassifyAreaDeepestNodeWins(t *testing.T) {
	tax := Taxonomy{
		Areas: []AreaNode{
			{
				ID: "backend", Label: "Backend",
				Match: MatchRule{PathPrefixes: []string{"internal/"}},
				Children: []AreaNode{
					{ID: "backend-registry", Label: "Registry", Match: MatchRule{PathPrefixes: []string{"internal/registry/"}}},
				},
			},
		},
	}
	if got := ClassifyArea(tax, Entry{Path: "internal/registry/service.go"}); got != "backend-registry" {
		t.Fatalf("expected the deeper child node to win, got %q", got)
	}
	if got := ClassifyArea(tax, Entry{Path: "internal/webserver/api.go"}); got != "backend" {
		t.Fatalf("expected the parent node when no child matches, got %q", got)
	}
	if got := ClassifyArea(tax, Entry{Path: "web/src/App.tsx"}); got != "" {
		t.Fatalf("expected no match outside internal/, got %q", got)
	}
}

func TestClassifyRoleFirstMatchWins(t *testing.T) {
	tax := Taxonomy{
		Roles: []RoleRule{
			{ID: "test", Match: MatchRule{PathGlobs: []string{"*_test.go"}}},
			{ID: "implementation", Match: MatchRule{PathGlobs: []string{"*.go"}}},
		},
	}
	if got := ClassifyRole(tax, Entry{Path: "a/b_test.go"}); got != "test" {
		t.Fatalf("expected test role to win over the broader implementation rule, got %q", got)
	}
}

func TestTaxonomyEditsSurviveRescan(t *testing.T) {
	svc, projectID, root := newTestService(t)
	writeSource(t, root, "a.go", "package a\n")
	if _, err := svc.Scan(projectID); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	tax, err := svc.LoadTaxonomy(projectID)
	if err != nil {
		t.Fatalf("LoadTaxonomy: %v", err)
	}
	tax.Areas = append(tax.Areas, AreaNode{ID: "custom", Label: "Custom", Match: MatchRule{PathPrefixes: []string{"a."}}})
	if err := svc.SaveTaxonomy(projectID, tax); err != nil {
		t.Fatalf("SaveTaxonomy: %v", err)
	}

	if _, err := svc.Scan(projectID); err != nil {
		t.Fatalf("rescan: %v", err)
	}
	after, err := svc.LoadTaxonomy(projectID)
	if err != nil {
		t.Fatalf("LoadTaxonomy after rescan: %v", err)
	}
	found := false
	for _, area := range after.Areas {
		if area.ID == "custom" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the hand-added area to survive a rescan, got %+v", after.Areas)
	}
}
