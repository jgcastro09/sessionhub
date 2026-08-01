package registry

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// SearchResult is one match, lexical, semantic, or fused.
type SearchResult struct {
	Entry        Entry    `json:"entry"`
	Score        int      `json:"score"`
	MatchReasons []string `json:"match_reasons,omitempty"`
}

// SearchQuery is a composite filter + free-text query over the registry.
// Every filter slice is OR'd internally and AND'd against the others (e.g.
// Categories=["go","docs"] + Languages=["go"] means "(go or docs category)
// and go language").
type SearchQuery struct {
	Text string

	Categories []string
	Modules    []string
	Areas      []string
	Languages  []string
	Roles      []string

	ReviewStatus []ReviewStatus
	Criticality  []Criticality
	Kind         []Kind
	Hash         string

	Semantic bool

	Limit  int
	Offset int
}

var tokenPattern = regexp.MustCompile(`[A-Za-z0-9_]+`)

func tokenize(s string) []string {
	return tokenPattern.FindAllString(strings.ToLower(s), -1)
}

// Search runs a composite filtered + full-text (and optionally semantic,
// Reciprocal-Rank-Fusion-merged) query. It degrades gracefully: if the
// SQLite index can't be opened, it falls back to an in-memory substring
// search (naiveSearch) over the same filters — search stays correct, just
// unranked, with nothing fancier ever load-bearing. Returns the page of
// results plus the total match count (pre-pagination).
func (s *Service) Search(projectID string, q SearchQuery) ([]SearchResult, int, error) {
	root, err := s.root(projectID)
	if err != nil {
		return nil, 0, err
	}
	entries, err := s.List(projectID, false)
	if err != nil {
		return nil, 0, err
	}
	byID := make(map[string]Entry, len(entries))
	for _, e := range entries {
		byID[e.EntryID] = e
	}

	db, dbErr := s.indexDB(root)
	if dbErr != nil {
		results := naiveSearch(entries, q)
		return paginateResults(results, q.Limit, q.Offset), len(results), nil
	}

	where, args := buildEntryFilter(q)
	rows, err := db.Query(fmt.Sprintf(`SELECT entry_id FROM entries_index WHERE %s`, where), args...) //nolint:gosec // where is built from a fixed clause set, args are parameterized
	if err != nil {
		results := naiveSearch(entries, q)
		return paginateResults(results, q.Limit, q.Offset), len(results), nil
	}
	filtered := map[string]bool{}
	for rows.Next() {
		var entryID string
		if err := rows.Scan(&entryID); err != nil {
			rows.Close()
			return nil, 0, err
		}
		if _, ok := byID[entryID]; ok {
			filtered[entryID] = true
		}
	}
	rows.Close()

	text := strings.TrimSpace(q.Text)
	if text == "" {
		ids := make([]string, 0, len(filtered))
		for id := range filtered {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool { return byID[ids[i]].Path < byID[ids[j]].Path })
		out := make([]SearchResult, len(ids))
		for i, id := range ids {
			out[i] = SearchResult{Entry: byID[id]}
		}
		return paginateResults(out, q.Limit, q.Offset), len(out), nil
	}

	cfg, err := loadConfig(root)
	if err != nil {
		return nil, 0, err
	}
	lexicalRanked, err := ftsSearch(db, text, cfg.SearchAliases, filtered)
	if err != nil {
		results := naiveSearch(entries, q)
		return paginateResults(results, q.Limit, q.Offset), len(results), nil
	}

	reasons := map[string][]string{}
	for _, r := range lexicalRanked {
		reasons[r.id] = append(reasons[r.id], "text match")
	}

	var semanticRanked []rankedID
	if q.Semantic && s.embedder != nil {
		if err := s.EnsureSemanticIndex(context.Background(), projectID); err == nil {
			if ranked, err := s.semanticRankedFiltered(root, text, filtered); err == nil {
				semanticRanked = ranked
				for _, r := range semanticRanked {
					reasons[r.id] = append(reasons[r.id], "semantic match")
				}
			}
		}
	}

	fused := reciprocalRankFusion(lexicalRanked, semanticRanked)
	out := make([]SearchResult, 0, len(fused))
	for _, f := range fused {
		e, ok := byID[f.id]
		if !ok {
			continue
		}
		out = append(out, SearchResult{Entry: e, Score: f.score, MatchReasons: reasons[f.id]})
	}
	return paginateResults(out, q.Limit, q.Offset), len(out), nil
}

func buildEntryFilter(q SearchQuery) (string, []any) {
	clauses := []string{"status = 'active'"}
	var args []any
	addIn := func(col string, values []string) {
		if len(values) == 0 {
			return
		}
		placeholders := make([]string, len(values))
		for i, v := range values {
			placeholders[i] = "?"
			args = append(args, v)
		}
		clauses = append(clauses, fmt.Sprintf("%s IN (%s)", col, strings.Join(placeholders, ",")))
	}
	addIn("category", q.Categories)
	addIn("module", q.Modules)
	addIn("area", q.Areas)
	addIn("language", q.Languages)
	addIn("role", q.Roles)
	if len(q.ReviewStatus) > 0 {
		vs := make([]string, len(q.ReviewStatus))
		for i, r := range q.ReviewStatus {
			vs[i] = string(r)
		}
		addIn("review_status", vs)
	}
	if len(q.Criticality) > 0 {
		vs := make([]string, len(q.Criticality))
		for i, c := range q.Criticality {
			vs[i] = string(c)
		}
		addIn("criticality", vs)
	}
	if len(q.Kind) > 0 {
		vs := make([]string, len(q.Kind))
		for i, k := range q.Kind {
			vs[i] = string(k)
		}
		addIn("kind", vs)
	}
	if q.Hash != "" {
		clauses = append(clauses, "hash = ?")
		args = append(args, q.Hash)
	}
	return strings.Join(clauses, " AND "), args
}

// rankedID is a 1-based rank position for Reciprocal Rank Fusion.
type rankedID struct {
	id   string
	rank int
}

// ftsSearch runs an FTS5 MATCH query, expanded with Config.SearchAliases,
// intersected with the already-filtered entry_id set (order-preserving by
// bm25 rank).
func ftsSearch(db *sql.DB, text string, aliases map[string][]string, filtered map[string]bool) ([]rankedID, error) {
	terms := tokenize(text)
	if len(terms) == 0 {
		return nil, nil
	}
	termSet := map[string]bool{}
	for _, t := range terms {
		termSet[t] = true
		for _, alias := range aliases[t] {
			termSet[strings.ToLower(alias)] = true
		}
	}
	var quoted []string
	for t := range termSet {
		t = strings.ReplaceAll(t, `"`, "")
		if t == "" {
			continue
		}
		quoted = append(quoted, `"`+t+`"*`)
	}
	if len(quoted) == 0 {
		return nil, nil
	}
	matchQuery := strings.Join(quoted, " OR ")

	rows, err := db.Query(`SELECT entry_id FROM fts_entries WHERE fts_entries MATCH ? ORDER BY bm25(fts_entries) LIMIT 2000`, matchQuery)
	if err != nil {
		return nil, fmt.Errorf("full-text search: %w", err)
	}
	defer rows.Close()
	var out []rankedID
	rank := 0
	for rows.Next() {
		var entryID string
		if err := rows.Scan(&entryID); err != nil {
			return nil, err
		}
		if !filtered[entryID] {
			continue
		}
		rank++
		out = append(out, rankedID{id: entryID, rank: rank})
	}
	return out, rows.Err()
}

func (s *Service) semanticRankedFiltered(root, query string, filtered map[string]bool) ([]rankedID, error) {
	db, err := s.indexDB(root)
	if err != nil {
		return nil, err
	}
	ctx := context.Background()
	queryVector, err := s.embedder.Embed(ctx, query)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `SELECT entry_id, vector FROM embeddings WHERE model = ?`, embeddingModel)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type scored struct {
		id    string
		score float64
	}
	var list []scored
	for rows.Next() {
		var entryID string
		var raw []byte
		if err := rows.Scan(&entryID, &raw); err != nil {
			return nil, err
		}
		if !filtered[entryID] {
			continue
		}
		list = append(list, scored{entryID, cosineSimilarity(queryVector, decodeVector(raw))})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].score > list[j].score })
	out := make([]rankedID, len(list))
	for i, sc := range list {
		out[i] = rankedID{id: sc.id, rank: i + 1}
	}
	return out, nil
}

type fusedResult struct {
	id    string
	score int
}

// reciprocalRankFusion merges ranked ID lists via RRF (score = sum of
// 1/(60+rank) across every list a result appears in), the standard way to
// combine heterogeneous rankers (lexical bm25 and semantic cosine aren't on
// the same scale, but their *ranks* are directly comparable).
func reciprocalRankFusion(lists ...[]rankedID) []fusedResult {
	scores := map[string]float64{}
	order := []string{}
	for _, list := range lists {
		for _, r := range list {
			if _, seen := scores[r.id]; !seen {
				order = append(order, r.id)
			}
			scores[r.id] += 1.0 / float64(60+r.rank)
		}
	}
	out := make([]fusedResult, len(order))
	for i, id := range order {
		out[i] = fusedResult{id: id, score: int(scores[id] * 100000)}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].score > out[j].score })
	return out
}

// naiveSearch is the always-correct fallback when the SQLite index is
// unavailable: an in-memory, deterministic substring/term-overlap search
// over the same SearchQuery filters. Nothing about the fancier layers is
// ever load-bearing — the system stays useful with just this.
func naiveSearch(entries []Entry, q SearchQuery) []SearchResult {
	filtered := make([]Entry, 0, len(entries))
	for _, e := range entries {
		if matchesFilters(e, q) {
			filtered = append(filtered, e)
		}
	}
	text := strings.TrimSpace(q.Text)
	if text == "" {
		sort.Slice(filtered, func(i, j int) bool { return filtered[i].Path < filtered[j].Path })
		out := make([]SearchResult, len(filtered))
		for i, e := range filtered {
			out[i] = SearchResult{Entry: e}
		}
		return out
	}
	terms := tokenize(text)
	var results []SearchResult
	for _, e := range filtered {
		haystack := tokenize(strings.Join(lexicalFields(e), " "))
		counts := map[string]int{}
		for _, tok := range haystack {
			counts[tok]++
		}
		score := 0
		for _, term := range terms {
			score += counts[term]
		}
		if score > 0 {
			results = append(results, SearchResult{Entry: e, Score: score, MatchReasons: []string{"text match"}})
		}
	}
	sort.SliceStable(results, func(i, j int) bool { return results[i].Score > results[j].Score })
	return results
}

func matchesFilters(e Entry, q SearchQuery) bool {
	if e.Status != StatusActive {
		return false
	}
	if len(q.Categories) > 0 && !contains(q.Categories, e.Category) {
		return false
	}
	if len(q.Modules) > 0 && !contains(q.Modules, e.Module) {
		return false
	}
	if len(q.Areas) > 0 && !contains(q.Areas, e.Area) {
		return false
	}
	if len(q.Languages) > 0 && !contains(q.Languages, e.Language) {
		return false
	}
	if len(q.Roles) > 0 && !contains(q.Roles, e.Role) {
		return false
	}
	if len(q.ReviewStatus) > 0 && !containsReviewStatus(q.ReviewStatus, e.ReviewStatus) {
		return false
	}
	if len(q.Criticality) > 0 && !containsCriticality(q.Criticality, e.Criticality) {
		return false
	}
	if len(q.Kind) > 0 && !containsKind(q.Kind, e.Kind) {
		return false
	}
	if q.Hash != "" && e.Hash != q.Hash {
		return false
	}
	return true
}

func containsReviewStatus(list []ReviewStatus, v ReviewStatus) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}

func containsCriticality(list []Criticality, v Criticality) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}

func containsKind(list []Kind, v Kind) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}

func lexicalFields(e Entry) []string {
	fields := []string{e.Path, e.Path, e.Module, e.Description}
	fields = append(fields, flattenSymbolNamesSlice(e.Symbols)...)
	fields = append(fields, e.Responsibilities...)
	return fields
}

func flattenSymbolNamesSlice(symbols map[string][]SymbolRef) []string {
	var out []string
	for _, refs := range symbols {
		for _, r := range refs {
			out = append(out, r.Name)
		}
	}
	return out
}

func flattenSymbolNames(symbols map[string][]SymbolRef) string {
	return strings.Join(flattenSymbolNamesSlice(symbols), " ")
}

func paginateResults(results []SearchResult, limit, offset int) []SearchResult {
	if offset < 0 {
		offset = 0
	}
	if offset >= len(results) {
		return nil
	}
	end := len(results)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	return results[offset:end]
}
