package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"smart_home/hub/internal/eventbus"
)

// SetEventBus attaches the event bus served over SSE.
func (s *Server) SetEventBus(bus *eventbus.Bus) { s.bus = bus }

// eventStream streams hub events to the browser as Server-Sent Events, so the
// UI updates without polling. It stays open until the client disconnects and
// sends a comment ping periodically to keep proxies from closing it.
func (s *Server) eventStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if s.bus == nil {
		writeError(w, http.StatusServiceUnavailable, "events_unavailable", "the event stream is not available")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "no_flush", "streaming is not supported")
		return
	}
	// Server-Sent Events are long-lived: clear the server write deadline for
	// this connection.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	ch, cancel := s.bus.Subscribe()
	defer cancel()

	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case ev, ok := <-ch:
			if !ok {
				return
			}
			data, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
