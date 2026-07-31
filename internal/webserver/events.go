package webserver

import (
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

	writeTick := func() bool {
		if _, err := fmt.Fprintf(w, "event: tick\ndata: {}\n\n"); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	if !writeTick() {
		return
	}

	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			if !writeTick() {
				return
			}
		}
	}
}
