package webserver

import (
	"encoding/json"
	"net/http"

	"github.com/jgcastro09/sessionhub/internal/tasks"
)

func (s *Server) handleTasksList(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("projectID")
	filter := tasks.Filter{
		ImpactedArea: r.URL.Query().Get("area"),
		RegistryRef:  r.URL.Query().Get("registry_ref"),
		Query:        r.URL.Query().Get("query"),
	}
	for _, v := range r.URL.Query()["status"] {
		filter.Status = append(filter.Status, tasks.Status(v))
	}
	for _, v := range r.URL.Query()["priority"] {
		filter.Priority = append(filter.Priority, tasks.Priority(v))
	}
	items, err := s.backend.WebTasksList(r.Context(), projectID, filter)
	if items == nil && err == nil {
		items = []tasks.Card{}
	}
	writeJSON(w, items, err)
}

func (s *Server) handleTasksCreate(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("projectID")
	var input tasks.CreateInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	card, err := s.backend.WebTasksCreate(r.Context(), projectID, input)
	if err != nil {
		writeJSONError(w, err)
		return
	}
	writeJSON(w, card, nil)
}

// handleTasksImport backs the "Importar card" action: the client pastes the
// standardized draft an AI assistant produced from the "Gerador de Cards"
// prompt contract, and this creates the card in one step when it validates.
// A parse failure is not an HTTP error — it is a normal 200 response with
// result.valid == false and result.errors describing exactly what to fix,
// same as any other client-side validation outcome.
func (s *Server) handleTasksImport(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	card, result, err := s.backend.WebTasksImport(r.Context(), r.PathValue("projectID"), body.Text)
	if err != nil {
		writeJSONError(w, err)
		return
	}
	response := struct {
		Card   *tasks.Card        `json:"card,omitempty"`
		Result tasks.ImportResult `json:"result"`
	}{Result: result}
	if result.Valid {
		response.Card = &card
	}
	writeJSON(w, response, nil)
}

func (s *Server) handleTasksGet(w http.ResponseWriter, r *http.Request) {
	card, err := s.backend.WebTasksGet(r.Context(), r.PathValue("projectID"), r.PathValue("taskID"))
	if err != nil {
		writeJSONError(w, err)
		return
	}
	writeJSON(w, card, nil)
}

// taskPatchBody accepts both card-field edits and a status change in one
// request; Patch's fields are inlined at the top level via Go's anonymous
// JSON embedding.
type taskPatchBody struct {
	tasks.Patch
	Status *tasks.Status `json:"status,omitempty"`
}

func (s *Server) handleTasksPatch(w http.ResponseWriter, r *http.Request) {
	projectID, taskID := r.PathValue("projectID"), r.PathValue("taskID")
	var body taskPatchBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var card tasks.Card
	var err error
	if !body.Patch.IsEmpty() {
		card, err = s.backend.WebTasksUpdate(r.Context(), projectID, taskID, body.Patch)
		if err != nil {
			writeJSONError(w, err)
			return
		}
	}
	if body.Status != nil {
		card, err = s.backend.WebTasksSetStatus(r.Context(), projectID, taskID, *body.Status)
		if err != nil {
			writeJSONError(w, err)
			return
		}
	}
	if body.Patch.IsEmpty() && body.Status == nil {
		card, err = s.backend.WebTasksGet(r.Context(), projectID, taskID)
		if err != nil {
			writeJSONError(w, err)
			return
		}
	}
	writeJSON(w, card, nil)
}

func (s *Server) handleTasksAudit(w http.ResponseWriter, r *http.Request) {
	report, err := s.backend.WebTasksAudit(r.Context(), r.PathValue("projectID"), r.PathValue("taskID"))
	if err != nil {
		writeJSONError(w, err)
		return
	}
	writeJSON(w, report, nil)
}

func (s *Server) handleTasksClaimsList(w http.ResponseWriter, r *http.Request) {
	claims, err := s.backend.WebTasksClaims(r.Context(), r.PathValue("projectID"))
	if claims == nil && err == nil {
		claims = []tasks.Claim{}
	}
	writeJSON(w, claims, err)
}

func (s *Server) handleTasksClaim(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ExecutorID string `json:"executor_id"`
		TerminalID string `json:"terminal_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	claims, err := s.backend.WebTasksClaim(r.Context(), r.PathValue("projectID"), r.PathValue("taskID"), body.ExecutorID, body.TerminalID)
	writeJSON(w, claims, err)
}

func (s *Server) handleTasksReleaseClaim(w http.ResponseWriter, r *http.Request) {
	claims, err := s.backend.WebTasksReleaseClaim(r.Context(), r.PathValue("projectID"), r.PathValue("terminalID"))
	writeJSON(w, claims, err)
}
