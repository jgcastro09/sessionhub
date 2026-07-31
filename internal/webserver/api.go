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
	api.HandleFunc("GET /api/sessions", s.handleSessions)
	api.HandleFunc("GET /api/executors", s.handleExecutors)
	api.HandleFunc("GET /api/executors/status", s.handleExecutorStatuses)
	api.HandleFunc("GET /api/metrics", s.handleMetrics)
	api.HandleFunc("GET /api/logs", s.handleLogs)
	api.HandleFunc("GET /api/queue", s.handleQueue)
	api.HandleFunc("GET /api/schedules", s.handleSchedules)
	api.HandleFunc("GET /api/pipelines", s.handlePipelines)
	api.HandleFunc("GET /api/events", s.handleEvents)
	mux.Handle("/api/", s.requireTrusted(api))

	// The SPA shell itself carries no session data — only the /api/*
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

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	items, err := s.backend.RemoteSessions(r.Context())
	if items == nil && err == nil {
		items = []domain.Session{}
	}
	writeJSON(w, items, err)
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
	sessionID := r.URL.Query().Get("session_id")
	metric, err := s.backend.RemoteMetrics(r.Context(), sessionID)
	writeJSON(w, metric, err)
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session_id")
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	items, err := s.backend.RemoteLogs(r.Context(), sessionID, limit)
	if items == nil && err == nil {
		items = []domain.LogEntry{}
	}
	writeJSON(w, items, err)
}

func (s *Server) handleQueue(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session_id")
	out := []domain.QueueItem{}
	err := s.forEachSession(r.Context(), sessionID, func(ctx context.Context, id string) error {
		items, err := s.backend.WebQueue(ctx, id)
		out = append(out, items...)
		return err
	})
	writeJSON(w, out, err)
}

func (s *Server) handleSchedules(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session_id")
	out := []domain.Schedule{}
	err := s.forEachSession(r.Context(), sessionID, func(ctx context.Context, id string) error {
		items, err := s.backend.WebSchedules(ctx, id)
		out = append(out, items...)
		return err
	})
	writeJSON(w, out, err)
}

func (s *Server) handlePipelines(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session_id")
	out := []domain.Pipeline{}
	err := s.forEachSession(r.Context(), sessionID, func(ctx context.Context, id string) error {
		items, err := s.backend.WebPipelines(ctx, id)
		out = append(out, items...)
		return err
	})
	writeJSON(w, out, err)
}

// forEachSession runs fn once for sessionID, or once per known session when
// sessionID is empty — the dashboard's "all sessions" view for data that
// internal/store only ever queries per-session.
func (s *Server) forEachSession(ctx context.Context, sessionID string, fn func(context.Context, string) error) error {
	ids := []string{sessionID}
	if sessionID == "" {
		sessions, err := s.backend.RemoteSessions(ctx)
		if err != nil {
			return err
		}
		ids = make([]string, len(sessions))
		for i, session := range sessions {
			ids[i] = session.ID
		}
	}
	for _, id := range ids {
		if err := fn(ctx, id); err != nil {
			return err
		}
	}
	return nil
}
