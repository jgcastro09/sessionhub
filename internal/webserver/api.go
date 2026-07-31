package webserver

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/jgcastro09/sessionhub/internal/domain"
)

func (s *Server) routes(mux *http.ServeMux) {
	// Pairing itself must stay reachable without a cookie — it's how a
	// device gets one — but everything else behind it needs a trusted
	// origin (loopback, Tailscale, or an already-paired LAN device).
	mux.HandleFunc("POST /api/pair", s.handlePair)

	api := http.NewServeMux()
	api.HandleFunc("GET /api/v2/projects", s.handleProjects)
	api.HandleFunc("GET /api/v2/projects/{projectID}", s.handleProject)
	api.HandleFunc("GET /api/v2/projects/{projectID}/executors", s.handleExecutors)
	api.HandleFunc("GET /api/v2/projects/{projectID}/metrics", s.handleMetrics)
	api.HandleFunc("GET /api/v2/projects/{projectID}/logs", s.handleLogs)
	api.HandleFunc("GET /api/v2/projects/{projectID}/queue", s.handleQueue)
	api.HandleFunc("GET /api/v2/projects/{projectID}/pipelines", s.handlePipelines)
	api.HandleFunc("GET /api/v2/projects/{projectID}/automations", s.handleSchedules)
	api.HandleFunc("GET /api/v2/projects/{projectID}/events", s.handleEvents)
	mux.Handle("/api/v2/", s.requireTrusted(api))

	// The SPA shell itself carries no project data — only the /api/*
	// endpoints above do — so it stays reachable pre-pairing. That's what
	// lets an unpaired LAN device load the page far enough to see the
	// PairingGate screen in the first place.
	mux.Handle("/", staticHandler())
}

// writeJSON always emits an array, never `null`, for slice-shaped responses
// so the frontend can .map() the body without a null check.
func writeJSON(w http.ResponseWriter, value any, err error) {
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func (s *Server) handleProjects(w http.ResponseWriter, r *http.Request) {
	items, err := s.backend.RemoteProjects(r.Context())
	if items == nil && err == nil {
		items = []domain.Project{}
	}
	writeJSON(w, items, err)
}

func (s *Server) handleProject(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("projectID")
	items, err := s.backend.RemoteProjects(r.Context())
	if err != nil {
		writeJSON(w, nil, err)
		return
	}
	for _, item := range items {
		if item.ID == projectID {
			writeJSON(w, item, nil)
			return
		}
	}
	http.Error(w, "project not found", http.StatusNotFound)
}

func (s *Server) handleExecutors(w http.ResponseWriter, r *http.Request) {
	items, err := s.backend.RemoteExecutors(r.Context())
	for i := range items {
		items[i] = items[i].Redacted()
	}
	if items == nil && err == nil {
		items = []domain.ExecutorConfig{}
	}
	writeJSON(w, items, err)
}

func (s *Server) handleExecutorStatuses(w http.ResponseWriter, r *http.Request) {
	items, err := s.backend.RemoteExecutorStatuses(r.Context())
	writeJSON(w, items, err)
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("projectID")
	metric, err := s.backend.RemoteMetrics(r.Context(), projectID)
	writeJSON(w, metric, err)
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("projectID")
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	items, err := s.backend.RemoteLogs(r.Context(), projectID, limit)
	if items == nil && err == nil {
		items = []domain.LogEntry{}
	}
	writeJSON(w, items, err)
}

func (s *Server) handleQueue(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("projectID")
	out := []domain.QueueItem{}
	err := s.forEachProject(r.Context(), projectID, func(ctx context.Context, id string) error {
		items, err := s.backend.WebQueue(ctx, id)
		out = append(out, items...)
		return err
	})
	writeJSON(w, out, err)
}

func (s *Server) handleSchedules(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("projectID")
	out := []domain.Schedule{}
	err := s.forEachProject(r.Context(), projectID, func(ctx context.Context, id string) error {
		items, err := s.backend.WebSchedules(ctx, id)
		out = append(out, items...)
		return err
	})
	writeJSON(w, out, err)
}

func (s *Server) handlePipelines(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("projectID")
	out := []domain.Pipeline{}
	err := s.forEachProject(r.Context(), projectID, func(ctx context.Context, id string) error {
		items, err := s.backend.WebPipelines(ctx, id)
		out = append(out, items...)
		return err
	})
	writeJSON(w, out, err)
}

// forEachProject runs fn once for projectID, or once per known project when
// projectID is empty — the dashboard's "all projects" view for data that
// internal/store only ever queries per-project.
func (s *Server) forEachProject(ctx context.Context, projectID string, fn func(context.Context, string) error) error {
	ids := []string{projectID}
	if projectID == "" {
		projects, err := s.backend.RemoteProjects(ctx)
		if err != nil {
			return err
		}
		ids = make([]string, len(projects))
		for i, project := range projects {
			ids[i] = project.ID
		}
	}
	for _, id := range ids {
		if err := fn(ctx, id); err != nil {
			return err
		}
	}
	return nil
}
