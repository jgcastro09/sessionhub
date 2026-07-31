package registry

import (
	"regexp"
	"sort"
	"strings"
)

var tokenPattern = regexp.MustCompile(`[A-Za-z0-9_]+`)

func tokenize(s string) []string {
	return tokenPattern.FindAllString(strings.ToLower(s), -1)
}

// SearchResult is one lexical (or, from Phase C.4 onward, semantic) match.
type SearchResult struct {
	Entry Entry `json:"entry"`
	Score int   `json:"score"`
}

// lexicalFields returns every text field of an entry that search indexes,
// most-significant-first (path and symbols weigh more than description).
func lexicalFields(e Entry) []string {
	fields := []string{e.Path, e.Path}
	fields = append(fields, e.Symbols...)
	fields = append(fields, e.Symbols...)
	fields = append(fields, e.Module, e.Description)
	fields = append(fields, e.Responsibilities...)
	return fields
}

// searchLexical ranks entries by token overlap with query. It is always
// available, including while a semantic model download is in progress or
// failed (plan 5.4 / section 9's determinism requirement), and it never
// returns a file that is not registered.
func searchLexical(entries []Entry, query string, limit int) []SearchResult {
	terms := tokenize(query)
	if len(terms) == 0 {
		return nil
	}
	var results []SearchResult
	for _, e := range entries {
		if e.Status != StatusActive {
			continue
		}
		haystack := tokenize(strings.Join(lexicalFields(e), " "))
		index := map[string]int{}
		for _, tok := range haystack {
			index[tok]++
		}
		score := 0
		for _, term := range terms {
			score += index[term]
		}
		if score > 0 {
			results = append(results, SearchResult{Entry: e, Score: score})
		}
	}
	sort.SliceStable(results, func(i, j int) bool { return results[i].Score > results[j].Score })
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results
}
