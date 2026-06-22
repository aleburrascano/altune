package app

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"altune/go-api/internal/auth"
	"altune/go-api/internal/shared/events"
)

// sseHandler streams a user's event bus over Server-Sent Events. It lives in the
// composition root because it is the seam between the cross-cutting event bus
// (shared/events) and the auth context — wiring those together is app/'s job, and
// keeping it here stops shared/events from importing auth (which would invert the
// dependency ring).
type sseHandler struct {
	bus events.Bus
}

func (h *sseHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	userId, ok := auth.RequireUserID(w, r)
	if !ok {
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	if lastID := r.Header.Get("Last-Event-ID"); lastID != "" {
		id, err := strconv.ParseUint(lastID, 10, 64)
		if err == nil {
			replayed := h.bus.Replay(userId, id)
			if replayed == nil {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			for _, evt := range replayed {
				writeSSEEvent(w, evt)
			}
			flusher.Flush()
		}
	}

	ch, cancel := h.bus.Subscribe(userId)
	defer cancel()

	slog.InfoContext(r.Context(), "sse.connected", "user_id", userId.String())

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			slog.InfoContext(r.Context(), "sse.disconnected", "user_id", userId.String())
			return
		case evt := <-ch:
			writeSSEEvent(w, evt)
			flusher.Flush()
		}
	}
}

func writeSSEEvent(w http.ResponseWriter, evt events.Event) {
	data, err := json.Marshal(evt.Payload)
	if err != nil {
		slog.Warn("sse.marshal_failed", "event_type", evt.Type, "error", err)
		return
	}
	fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", evt.ID, evt.Type, data)
}
