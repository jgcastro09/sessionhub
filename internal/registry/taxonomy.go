package registry

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/jgcastro09/sessionhub/internal/atomicfile"
)

// MatchRule is an OR of independent match criteria: a rule matches an entry
// if any one of its non-empty fields matches. PathGlobs are checked against
// both the full path and the basename, so "*.md" and "docs/**" both behave
// as expected. TextTerms match case-insensitively against the entry's
// (human-authored) Description and Responsibilities — new/unreviewed files
// simply won't match on text terms yet, which is fine: Role/Area are
// recomputed every scan, so a file picks up its text-term-based
// classification the moment it's reviewed.
type MatchRule struct {
	PathGlobs      []string `json:"path_globs,omitempty"`
	PathPrefixes   []string `json:"path_prefixes,omitempty"`
	ModulePrefixes []string `json:"module_prefixes,omitempty"`
	ExactPaths     []string `json:"exact_paths,omitempty"`
	TextTerms      []string `json:"text_terms,omitempty"`
}

func (m MatchRule) matches(e Entry) bool {
	if len(m.ExactPaths) > 0 && contains(m.ExactPaths, e.Path) {
		return true
	}
	if len(m.PathPrefixes) > 0 && hasAnyPrefix(e.Path, m.PathPrefixes) {
		return true
	}
	if len(m.ModulePrefixes) > 0 && hasAnyPrefix(e.Module, m.ModulePrefixes) {
		return true
	}
	for _, glob := range m.PathGlobs {
		if ok, _ := path.Match(glob, e.Path); ok {
			return true
		}
		if ok, _ := path.Match(glob, path.Base(e.Path)); ok {
			return true
		}
	}
	if len(m.TextTerms) > 0 {
		haystack := strings.ToLower(e.Description + " " + strings.Join(e.Responsibilities, " "))
		if haystack != "" {
			for _, term := range m.TextTerms {
				if term != "" && strings.Contains(haystack, strings.ToLower(term)) {
					return true
				}
			}
		}
	}
	return false
}

// RoleRule assigns an architectural Role, first-match-wins, unranked (an
// entry may legitimately match no role — Role stays empty, which is a
// finding health.go can surface, not an error).
type RoleRule struct {
	ID    string    `json:"id"`
	Label string    `json:"label"`
	Match MatchRule `json:"match"`
}

// AreaNode is one node of a hierarchical product-area tree. A child's match
// is checked before its parent's, so refinement rules can override a coarse
// parent without repeating the parent's own criteria — the deepest matching
// node wins.
type AreaNode struct {
	ID       string     `json:"id"`
	Label    string     `json:"label"`
	Match    MatchRule  `json:"match"`
	Children []AreaNode `json:"children,omitempty"`
}

// Taxonomy is the declarative, external layer that assigns architectural
// Role and product Area without duplicating tags into every entry record —
// re-tagging is a one-file edit to taxonomy.json, not a mass rewrite of
// records/*.json. It is seeded once (seedTaxonomy) and never auto-rewritten
// by Scan afterward, so user edits are never clobbered.
type Taxonomy struct {
	Roles []RoleRule `json:"roles"`
	Areas []AreaNode `json:"areas"`
}

// ClassifyRole returns the ID of the first matching role, or "" if none
// match.
func ClassifyRole(t Taxonomy, e Entry) string {
	for _, r := range t.Roles {
		if r.Match.matches(e) {
			return r.ID
		}
	}
	return ""
}

// ClassifyArea returns the ID of the deepest matching area node, or "" if
// none match.
func ClassifyArea(t Taxonomy, e Entry) string {
	for _, area := range t.Areas {
		if id, ok := classifyAreaNode(area, e); ok {
			return id
		}
	}
	return ""
}

func classifyAreaNode(node AreaNode, e Entry) (string, bool) {
	for _, child := range node.Children {
		if id, ok := classifyAreaNode(child, e); ok {
			return id, true
		}
	}
	if node.Match.matches(e) {
		return node.ID, true
	}
	return "", false
}

// LoadTaxonomy reads taxonomy.json, seeding and persisting a generic
// default the first time it's called for a project. Every call after that
// simply reads whatever is on disk — Scan never rewrites this file, so
// hand-edited roles/areas survive indefinitely.
func LoadTaxonomy(root string) (Taxonomy, error) {
	data, err := os.ReadFile(taxonomyPath(root))
	if os.IsNotExist(err) {
		t := defaultTaxonomy()
		if writeErr := SaveTaxonomy(root, t); writeErr != nil {
			return Taxonomy{}, writeErr
		}
		return t, nil
	}
	if err != nil {
		return Taxonomy{}, err
	}
	var t Taxonomy
	if err := json.Unmarshal(data, &t); err != nil {
		return Taxonomy{}, fmt.Errorf("decode taxonomy: %w", err)
	}
	return t, nil
}

// SaveTaxonomy writes taxonomy.json atomically. Only ever called by
// LoadTaxonomy's first-run seed and by the Web Panel's Config editor
// (Phase 9/10) — Scan itself never calls this.
func SaveTaxonomy(root string, t Taxonomy) error {
	return atomicfile.WriteJSON(taxonomyPath(root), t)
}

// defaultTaxonomy seeds a new project with a small, generic role/area set
// covering common repository conventions. Users are expected to refine it
// via the Config view once their project's real structure is scanned.
func defaultTaxonomy() Taxonomy {
	return Taxonomy{
		Roles: []RoleRule{
			{ID: "test", Label: "Test", Match: MatchRule{PathGlobs: defaultTestGlobs}},
			{ID: "build", Label: "Build & Release", Match: MatchRule{PathPrefixes: []string{"scripts/", "installer/", ".github/"}, PathGlobs: defaultBuildFilenames}},
			{ID: "tool", Label: "Developer Tools", Match: MatchRule{PathPrefixes: []string{"tools/", "cmd/"}}},
			{ID: "docs", Label: "Documentation", Match: MatchRule{PathGlobs: []string{"*.md", "*.mdx"}}},
			{ID: "config", Label: "Configuration", Match: MatchRule{PathGlobs: []string{"*.json", "*.yaml", "*.yml", "*.toml"}}},
			{ID: "ui", Label: "User Interface", Match: MatchRule{PathPrefixes: []string{"web/"}, PathGlobs: []string{"*.tsx", "*.jsx", "*.css", "*.scss"}}},
			{ID: "service", Label: "Service / Backend", Match: MatchRule{PathPrefixes: []string{"internal/", "cmd/", "pkg/"}}},
			{ID: "implementation", Label: "Implementation", Match: MatchRule{PathGlobs: []string{"*"}}},
		},
		Areas: []AreaNode{
			{ID: "backend", Label: "Backend", Match: MatchRule{PathPrefixes: []string{"internal/", "cmd/", "pkg/"}}},
			{ID: "frontend", Label: "Frontend", Match: MatchRule{PathPrefixes: []string{"web/"}}},
			{ID: "docs", Label: "Documentation", Match: MatchRule{PathPrefixes: []string{"docs/"}, PathGlobs: []string{"*.md"}}},
			{ID: "tooling", Label: "Tooling & Scripts", Match: MatchRule{PathPrefixes: []string{"tools/", "scripts/"}}},
		},
	}
}
