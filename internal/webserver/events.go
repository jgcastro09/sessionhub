package webserver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// tickInterval is how often /api/events nudges connected clients to refetch.
// v1 doesn't hook into every store write across executor/automation/context
// (that's a much larger change for later); a lightweight periodic "something
// may have changed" push over a single long-lived connection already beats
// each view running its own setInterval poll, and refetches stay cheap since
// the monitoring endpoints are simple reads.
const tickInterval = 3 * time.Second

// handleEvents streams Server-Sent Events so the frontend can replace
// per-view polling with a single push channel (see web/src/hooks/useLiveTick.ts).
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	projectID := r.PathValue("projectID")
	writeTick := func() bool {
		if _, err := fmt.Fprintf(w, "event: tick\ndata: {\"project_id\":%q}\n\n", projectID); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	if !writeTick() {
		return
	}

	// Task/Registry writes publish a typed event immediately (project_id,
	// kind, revision, a small payload) — the heartbeat above stays as a
	// fallback for views that haven't been migrated off polling yet and for
	// reconnection after a dropped connection.
	feed, cancel := s.backend.Subscribe(projectID)
	defer cancel()

	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-s.ctx.Done():
			return
		case event, ok := <-feed:
			if !ok {
				return
			}
			data, err := json.Marshal(event)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Kind, data); err != nil {
				return
			}
			flusher.Flush()
		case <-ticker.C:
			if !writeTick() {
				return
			}
		}
	}
}
