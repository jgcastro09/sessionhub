package webserver

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/jgcastro09/sessionhub/internal/gitstate"
	"github.com/jgcastro09/sessionhub/internal/registry"
)

func (s *Server) handleRegistryConfigGet(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.backend.WebRegistryConfigGet(r.Context(), r.PathValue("projectID"))
	writeJSON(w, cfg, err)
}

func (s *Server) handleRegistryConfigPut(w http.ResponseWriter, r *http.Request) {
	var cfg registry.Config
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.backend.WebRegistryConfigPut(r.Context(), r.PathValue("projectID"), cfg); err != nil {
		if err := cfg.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSONError(w, err)
		return
	}
	writeJSON(w, cfg, nil)
}

func (s *Server) handleRegistryTaxonomyGet(w http.ResponseWriter, r *http.Request) {
	tax, err := s.backend.WebRegistryTaxonomyGet(r.Context(), r.PathValue("projectID"))
	writeJSON(w, tax, err)
}

func (s *Server) handleRegistryTaxonomyPut(w http.ResponseWriter, r *http.Request) {
	var tax registry.Taxonomy
	if err := json.NewDecoder(r.Body).Decode(&tax); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.backend.WebRegistryTaxonomyPut(r.Context(), r.PathValue("projectID"), tax); err != nil {
		writeJSONError(w, err)
		return
	}
	writeJSON(w, tax, nil)
}

func (s *Server) handleRegistryList(w http.ResponseWriter, r *http.Request) {
	q := parseSearchQuery(r)
	results, total, err := s.backend.WebRegistrySearch(r.Context(), r.PathValue("projectID"), q)
	writeJSON(w, searchPage{Results: results, Total: total}, err)
}

func (s *Server) handleRegistryGet(w http.ResponseWriter, r *http.Request) {
	entry, err := s.backend.WebRegistryGet(r.Context(), r.PathValue("projectID"), r.PathValue("entryID"))
	writeJSONOrError(w, entry, err)
}

func (s *Server) handleRegistryScan(w http.ResponseWriter, r *http.Request) {
	entries, err := s.backend.WebRegistryScan(r.Context(), r.PathValue("projectID"))
	writeJSON(w, entries, err)
}

func (s *Server) handleRegistryHealth(w http.ResponseWriter, r *http.Request) {
	report, err := s.backend.WebRegistryHealth(r.Context(), r.PathValue("projectID"))
	writeJSON(w, report, err)
}

func (s *Server) handleRegistryEnsureFresh(w http.ResponseWriter, r *http.Request) {
	result, err := s.backend.WebRegistryEnsureFresh(r.Context(), r.PathValue("projectID"))
	writeJSON(w, result, err)
}

func (s *Server) handleRegistryStats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.backend.WebRegistryStats(r.Context(), r.PathValue("projectID"))
	writeJSON(w, stats, err)
}

func (s *Server) handleRegistryPending(w http.ResponseWriter, r *http.Request) {
	pending, err := s.backend.WebRegistryPending(r.Context(), r.PathValue("projectID"))
	if pending == nil && err == nil {
		pending = []registry.PendingFile{}
	}
	writeJSON(w, pending, err)
}

func (s *Server) handleRegistryReviewQueue(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePagination(r, 50)
	entries, total, err := s.backend.WebRegistryReviewQueue(r.Context(), r.PathValue("projectID"), limit, offset)
	writeJSON(w, searchPage{Results: entriesToResults(entries), Total: total}, err)
}

type searchPage struct {
	Results []registry.SearchResult `json:"results"`
	Total   int                     `json:"total"`
}

func entriesToResults(entries []registry.Entry) []registry.SearchResult {
	out := make([]registry.SearchResult, len(entries))
	for i, e := range entries {
		out[i] = registry.SearchResult{Entry: e}
	}
	return out
}

func (s *Server) handleRegistrySearch(w http.ResponseWriter, r *http.Request) {
	q := parseSearchQuery(r)
	results, total, err := s.backend.WebRegistrySearch(r.Context(), r.PathValue("projectID"), q)
	writeJSON(w, searchPage{Results: results, Total: total}, err)
}

func parseSearchQuery(r *http.Request) registry.SearchQuery {
	values := r.URL.Query()
	limit, offset := parsePagination(r, 100)
	q := registry.SearchQuery{
		Text:       values.Get("query"),
		Categories: splitQueryList(values.Get("category")),
		Modules:    splitQueryList(values.Get("module")),
		Areas:      splitQueryList(values.Get("area")),
		Languages:  splitQueryList(values.Get("language")),
		Roles:      splitQueryList(values.Get("role")),
		Hash:       values.Get("hash"),
		Semantic:   values.Get("semantic") == "true",
		Limit:      limit,
		Offset:     offset,
	}
	for _, v := range splitQueryList(values.Get("review_status")) {
		q.ReviewStatus = append(q.ReviewStatus, registry.ReviewStatus(v))
	}
	for _, v := range splitQueryList(values.Get("criticality")) {
		q.Criticality = append(q.Criticality, registry.Criticality(v))
	}
	for _, v := range splitQueryList(values.Get("kind")) {
		q.Kind = append(q.Kind, registry.Kind(v))
	}
	return q
}

func splitQueryList(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func parsePagination(r *http.Request, defaultLimit int) (limit, offset int) {
	limit = defaultLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	if raw := r.URL.Query().Get("offset"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed >= 0 {
			offset = parsed
		}
	}
	return limit, offset
}

func (s *Server) handleRegistrySemanticSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("query")
	limit, _ := parsePagination(r, 20)
	results, err := s.backend.WebRegistrySemanticSearch(r.Context(), r.PathValue("projectID"), query, limit)
	writeJSON(w, results, err)
}

func (s *Server) handleRegistryGraph(w http.ResponseWriter, r *http.Request) {
	graph, issues, err := s.backend.WebRegistryGraph(r.Context(), r.PathValue("projectID"))
	writeJSON(w, graphWithIssues{Graph: graph, DependencyIssues: issues}, err)
}

type graphWithIssues struct {
	registry.Graph
	DependencyIssues []registry.DependencyIssue `json:"dependency_issues"`
}

func (s *Server) handleRegistryEntryGraph(w http.ResponseWriter, r *http.Request) {
	depth := 2
	limit := 150
	if raw := r.URL.Query().Get("depth"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			depth = parsed
		}
	}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	graph, err := s.backend.WebRegistryEntryGraph(r.Context(), r.PathValue("projectID"), r.PathValue("entryID"), depth, limit)
	writeJSON(w, graph, err)
}

func (s *Server) handleRegistrySource(w http.ResponseWriter, r *http.Request) {
	source, err := s.backend.WebRegistrySource(r.Context(), r.PathValue("projectID"), r.PathValue("entryID"))
	if err != nil {
		writeJSONError(w, err)
		return
	}
	writeJSON(w, map[string]string{"content": source}, nil)
}

func (s *Server) handleRegistrySourceHistory(w http.ResponseWriter, r *http.Request) {
	limit, _ := parsePagination(r, 50)
	revisions, err := s.backend.WebRegistrySourceHistory(r.Context(), r.PathValue("projectID"), r.PathValue("entryID"), limit)
	if revisions == nil && err == nil {
		revisions = []gitstate.FileRevision{}
	}
	writeJSON(w, revisions, err)
}

func (s *Server) handleRegistrySourceAtRevision(w http.ResponseWriter, r *http.Request) {
	ref := r.URL.Query().Get("ref")
	if ref == "" {
		http.Error(w, "ref query parameter is required", http.StatusBadRequest)
		return
	}
	source, err := s.backend.WebRegistrySourceAtRevision(r.Context(), r.PathValue("projectID"), r.PathValue("entryID"), ref)
	if err != nil {
		writeJSONError(w, err)
		return
	}
	writeJSON(w, map[string]string{"content": source}, nil)
}

func (s *Server) handleRegistryGitStatus(w http.ResponseWriter, r *http.Request) {
	correlation, err := s.backend.WebRegistryGitStatus(r.Context(), r.PathValue("projectID"))
	writeJSON(w, correlation, err)
}

func (s *Server) handleRegistryReview(w http.ResponseWriter, r *http.Request) {
	var input registry.ReviewInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	entry, err := s.backend.WebRegistryReview(r.Context(), r.PathValue("projectID"), r.PathValue("entryID"), input)
	writeJSONOrError(w, entry, err)
}

func writeJSONOrError(w http.ResponseWriter, value any, err error) {
	if err != nil {
		writeJSONError(w, err)
		return
	}
	writeJSON(w, value, nil)
}
