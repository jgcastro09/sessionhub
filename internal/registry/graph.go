package registry

import (
	"path"
	"strings"
)

// GraphNode is one node in the relationship graph: either a file (an
// Entry) or a synthetic container (an Area or Module), so the graph can
// render containment without a separate data structure.
type GraphNode struct {
	ID           string       `json:"id"`
	Kind         string       `json:"kind"` // "file" | "area" | "module"
	Label        string       `json:"label"`
	Path         string       `json:"path,omitempty"`
	Category     string       `json:"category,omitempty"`
	ReviewStatus ReviewStatus `json:"review_status,omitempty"`
	Criticality  Criticality  `json:"criticality,omitempty"`
}

// GraphEdge connects two node IDs. Kind is one of "contains", "implements",
// "imports", "includes", "related", "probable". Confidence distinguishes a
// human-confirmed edge ("confirmed"), a heuristically-resolved import
// ("resolved"), a same-stem heuristic ("probable"), or a dropped-not-guessed
// ambiguous match (never added as an edge — see DependencyIssue instead).
type GraphEdge struct {
	From       string `json:"from"`
	To         string `json:"to"`
	Kind       string `json:"kind"`
	Confidence string `json:"confidence"`
}

// Graph is the full computed relationship graph for a set of entries.
type Graph struct {
	Nodes []GraphNode `json:"nodes"`
	Edges []GraphEdge `json:"edges"`
}

// DependencyIssue records a raw Includes/Imports string that could not be
// resolved to exactly one other entry — either nothing matched
// ("unresolved", most often an external/stdlib/third-party reference, which
// is expected and not itself a problem) or more than one candidate matched
// ("ambiguous", which health.go surfaces since it means the graph had to
// drop a real edge rather than guess).
type DependencyIssue struct {
	EntryID   string `json:"entry_id"`
	Path      string `json:"path"`
	RawImport string `json:"raw_import"`
	Reason    string `json:"reason"` // "ambiguous" | "unresolved"
}

// BuildGraph computes the full relationship graph fresh from a set of
// entries. Nothing here is persisted per-entry — edges are recomputed at
// query time, so a Taxonomy/Config change is reflected immediately without
// needing a rescan.
func BuildGraph(entries []Entry) (Graph, []DependencyIssue) {
	var g Graph
	var issues []DependencyIssue

	active := make([]Entry, 0, len(entries))
	for _, e := range entries {
		if e.Status != StatusActive {
			continue
		}
		active = append(active, e)
		g.Nodes = append(g.Nodes, GraphNode{
			ID: e.EntryID, Kind: "file", Label: e.Filename, Path: e.Path,
			Category: e.Category, ReviewStatus: e.ReviewStatus, Criticality: e.Criticality,
		})
	}

	seenEdge := map[string]bool{}
	addEdge := func(from, to, kind, confidence string) {
		if from == "" || to == "" || from == to {
			return
		}
		key := from + "\x00" + to + "\x00" + kind
		if seenEdge[key] {
			return
		}
		seenEdge[key] = true
		g.Edges = append(g.Edges, GraphEdge{From: from, To: to, Kind: kind, Confidence: confidence})
	}

	for _, e := range active {
		for _, rel := range e.RelatedFiles {
			addEdge(e.EntryID, rel, "related", "confirmed")
		}
		for _, rel := range e.ProbableRelatedFiles {
			addEdge(e.EntryID, rel, "probable", "probable")
		}
	}

	// implements: header/implementation pairing by same (dir, stem).
	groups := map[string][]Entry{}
	for _, e := range active {
		key := stemKey(e.Path)
		groups[key] = append(groups[key], e)
	}
	for _, group := range groups {
		var headers, impls []Entry
		for _, e := range group {
			switch e.Kind {
			case KindHeader:
				headers = append(headers, e)
			case KindImplementation:
				impls = append(impls, e)
			}
		}
		for _, h := range headers {
			for _, im := range impls {
				addEdge(h.EntryID, im.EntryID, "implements", "confirmed")
				addEdge(im.EntryID, h.EntryID, "implements", "confirmed")
			}
		}
	}

	// imports/includes: resolve raw strings against other entries' paths.
	byPath := make(map[string]Entry, len(active))
	bySuffix := map[string][]Entry{}
	for _, e := range active {
		byPath[e.Path] = e
		segments := strings.Split(e.Path, "/")
		for i := range segments {
			suffix := strings.Join(segments[i:], "/")
			bySuffix[suffix] = append(bySuffix[suffix], e)
		}
	}
	resolve := func(from Entry, raw, kind string) {
		target, ambiguous := resolveImport(from, raw, byPath, bySuffix)
		if target != nil {
			addEdge(from.EntryID, target.EntryID, kind, "resolved")
			return
		}
		if ambiguous {
			issues = append(issues, DependencyIssue{EntryID: from.EntryID, Path: from.Path, RawImport: raw, Reason: "ambiguous"})
		}
		// A plain "unresolved" (no candidate at all) is expected for
		// stdlib/third-party references and is not recorded as an issue —
		// only an ambiguous, dropped-not-guessed match is a real finding.
	}
	for _, e := range active {
		for _, raw := range e.Includes {
			resolve(e, raw, "includes")
		}
		for _, raw := range e.Imports {
			resolve(e, raw, "imports")
		}
	}

	// contains: synthetic Area/Module container nodes.
	moduleNodes := map[string]bool{}
	areaNodes := map[string]bool{}
	for _, e := range active {
		if e.Module != "" {
			moduleID := "module:" + e.Module
			if !moduleNodes[moduleID] {
				moduleNodes[moduleID] = true
				g.Nodes = append(g.Nodes, GraphNode{ID: moduleID, Kind: "module", Label: e.Module})
			}
			addEdge(moduleID, e.EntryID, "contains", "confirmed")
		}
		if e.Area != "" {
			areaID := "area:" + e.Area
			if !areaNodes[areaID] {
				areaNodes[areaID] = true
				g.Nodes = append(g.Nodes, GraphNode{ID: areaID, Kind: "area", Label: e.Area})
			}
			addEdge(areaID, e.EntryID, "contains", "confirmed")
		}
	}

	return g, issues
}

// resolveImport tries, in order: the raw string joined against the
// importing file's own directory, the raw string as project-root-relative,
// either of those with a guessed extension appended, and finally a unique
// suffix-index lookup. A suffix match with more than one candidate is
// reported as ambiguous and dropped rather than guessed.
func resolveImport(from Entry, raw string, byPath map[string]Entry, bySuffix map[string][]Entry) (*Entry, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, false
	}
	extGuesses := []string{".go", ".ts", ".tsx", ".js", ".jsx", ".py", ".h", ".hpp", ".c", ".cpp", ".rs"}

	candidates := candidatePaths(from, raw)
	for _, c := range candidates {
		if e, ok := byPath[c]; ok {
			return &e, false
		}
	}
	if path.Ext(raw) == "" {
		for _, c := range candidates {
			for _, ext := range extGuesses {
				if e, ok := byPath[c+ext]; ok {
					return &e, false
				}
			}
		}
	}

	// bySuffix is keyed by path segment suffixes as they actually appear on
	// disk, extension included — a bare import like "shared" (no extension)
	// needs the same extension-guessing treatment to land on "shared.go".
	suffix := strings.TrimPrefix(strings.TrimPrefix(raw, "./"), "/")
	suffixCandidates := []string{suffix}
	if path.Ext(suffix) == "" {
		for _, ext := range extGuesses {
			suffixCandidates = append(suffixCandidates, suffix+ext)
		}
	}
	for _, sc := range suffixCandidates {
		matches, ok := bySuffix[sc]
		if !ok {
			continue
		}
		if len(matches) == 1 {
			return &matches[0], false
		}
		return nil, true // ambiguous: more than one file shares this suffix
	}
	return nil, false
}

func candidatePaths(from Entry, raw string) []string {
	dir := path.Dir(from.Path)
	if dir == "." {
		dir = ""
	}
	return []string{
		path.Clean(path.Join(dir, raw)),
		strings.TrimPrefix(raw, "/"),
	}
}

// ImpactAnalysis returns the bounded subgraph reachable from seedEntryID
// within depth hops, truncated to at most nodeLimit nodes. When truncating,
// needs_review and critical nodes are kept preferentially over
// already-reviewed/standard ones, so the most important context survives a
// large fan-out.
func ImpactAnalysis(g Graph, seedEntryID string, depth, nodeLimit int) Graph {
	if depth <= 0 {
		depth = 2
	}
	if nodeLimit <= 0 {
		nodeLimit = 150
	}
	adjacency := map[string][]string{}
	for _, e := range g.Edges {
		adjacency[e.From] = append(adjacency[e.From], e.To)
		adjacency[e.To] = append(adjacency[e.To], e.From)
	}
	nodeByID := make(map[string]GraphNode, len(g.Nodes))
	for _, n := range g.Nodes {
		nodeByID[n.ID] = n
	}
	if _, ok := nodeByID[seedEntryID]; !ok {
		return Graph{}
	}

	type frontierNode struct {
		id    string
		level int
	}
	visited := map[string]int{seedEntryID: 0}
	queue := []frontierNode{{seedEntryID, 0}}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur.level >= depth {
			continue
		}
		for _, next := range adjacency[cur.id] {
			if _, seen := visited[next]; seen {
				continue
			}
			visited[next] = cur.level + 1
			queue = append(queue, frontierNode{next, cur.level + 1})
		}
	}

	ids := make([]string, 0, len(visited))
	for id := range visited {
		ids = append(ids, id)
	}
	if len(ids) > nodeLimit {
		rankOf := func(id string) int {
			n, ok := nodeByID[id]
			if !ok {
				return 0
			}
			rank := 0
			if n.ReviewStatus == ReviewNeedsReview {
				rank += 2
			}
			if n.Criticality == CriticalityCritical {
				rank += 2
			} else if n.Criticality == CriticalityImportant {
				rank++
			}
			return rank
		}
		// Stable partial sort: seed always first, then by (rank desc, level asc).
		sortByRankThenLevel(ids, seedEntryID, rankOf, visited)
		ids = ids[:nodeLimit]
	}

	keep := make(map[string]bool, len(ids))
	for _, id := range ids {
		keep[id] = true
	}
	var out Graph
	for _, id := range ids {
		if n, ok := nodeByID[id]; ok {
			out.Nodes = append(out.Nodes, n)
		}
	}
	for _, e := range g.Edges {
		if keep[e.From] && keep[e.To] {
			out.Edges = append(out.Edges, e)
		}
	}
	return out
}

func sortByRankThenLevel(ids []string, seedID string, rankOf func(string) int, level map[string]int) {
	// Simple insertion sort: node counts here are bounded by nodeLimit
	// callers pass (defaults to 150), never large enough to need anything
	// fancier.
	for i := 1; i < len(ids); i++ {
		j := i
		for j > 0 && lessGraphPriority(ids[j], ids[j-1], seedID, rankOf, level) {
			ids[j], ids[j-1] = ids[j-1], ids[j]
			j--
		}
	}
}

func lessGraphPriority(a, b, seedID string, rankOf func(string) int, level map[string]int) bool {
	if a == seedID {
		return true
	}
	if b == seedID {
		return false
	}
	ra, rb := rankOf(a), rankOf(b)
	if ra != rb {
		return ra > rb
	}
	return level[a] < level[b]
}
