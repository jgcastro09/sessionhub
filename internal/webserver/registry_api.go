package webserver

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/jgcastro09/sessionhub/internal/registry"
)

func (s *Server) handleRegistryList(w http.ResponseWriter, r *http.Request) {
	items, err := s.backend.WebRegistryList(r.Context(), r.PathValue("projectID"))
	writeJSON(w, registry.RedactedEntries(items), err)
}

func (s *Server) handleRegistryGet(w http.ResponseWriter, r *http.Request) {
	entry, err := s.backend.WebRegistryGet(r.Context(), r.PathValue("projectID"), r.PathValue("entryID"))
	if err != nil {
		writeJSONError(w, err)
		return
	}
	writeJSON(w, entry.Redacted(), nil)
}

func (s *Server) handleRegistryScan(w http.ResponseWriter, r *http.Request) {
	entries, err := s.backend.WebRegistryScan(r.Context(), r.PathValue("projectID"))
	writeJSON(w, registry.RedactedEntries(entries), err)
}

func (s *Server) handleRegistryHealth(w http.ResponseWriter, r *http.Request) {
	report, err := s.backend.WebRegistryHealth(r.Context(), r.PathValue("projectID"))
	writeJSON(w, report, err)
}

func (s *Server) handleRegistrySearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("query")
	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	var results []registry.SearchResult
	var err error
	if r.URL.Query().Get("semantic") == "true" {
		results, err = s.backend.WebRegistrySemanticSearch(r.Context(), r.PathValue("projectID"), query, limit)
	} else {
		results, err = s.backend.WebRegistrySearch(r.Context(), r.PathValue("projectID"), query, limit)
	}
	writeJSON(w, redactResults(results), err)
}

func redactResults(results []registry.SearchResult) []registry.SearchResult {
	out := make([]registry.SearchResult, len(results))
	for i, r := range results {
		out[i] = registry.SearchResult{Entry: r.Entry.Redacted(), Score: r.Score}
	}
	return out
}

func (s *Server) handleRegistryContext(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("projectID")
	entryID := r.URL.Query().Get("entry_id")
	if entryID == "" {
		http.Error(w, "entry_id query parameter is required", http.StatusBadRequest)
		return
	}
	entry, err := s.backend.WebRegistryGet(r.Context(), projectID, entryID)
	if err != nil {
		writeJSONError(w, err)
		return
	}
	related, err := s.backend.WebRegistrySearch(r.Context(), projectID, entry.Module+" "+joinSymbols(entry), 10)
	if err != nil {
		writeJSONError(w, err)
		return
	}
	pack := registry.ContextPack{
		Entry:   entry.Redacted(),
		Related: redactResults(registry.SearchResults(related).Filter(entry.EntryID)),
	}
	writeJSON(w, pack, nil)
}

func joinSymbols(e registry.Entry) string {
	out := ""
	for _, s := range e.Symbols {
		out += s + " "
	}
	return out
}

func (s *Server) handleRegistrySource(w http.ResponseWriter, r *http.Request) {
	source, err := s.backend.WebRegistrySource(r.Context(), r.PathValue("projectID"), r.PathValue("entryID"))
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
	if err != nil {
		writeJSONError(w, err)
		return
	}
	writeJSON(w, entry.Redacted(), nil)
}
