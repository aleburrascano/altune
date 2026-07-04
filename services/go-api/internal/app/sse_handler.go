package app

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"altune/go-api/internal/auth"
	"altune/go-api/internal/shared/events"
)

// defaultHeartbeatInterval is how often an idle stream emits a comment ping so
// proxy/carrier idle-timeouts don't silently drop the socket and the client
// watchdog has a signal to reconnect on.
const defaultHeartbeatInterval = 25 * time.Second

// sseHandler streams a user's event bus over Server-Sent Events. It lives in the
// composition root because it is the seam between the cross-cutting event bus
// (shared/events) and the auth context — wiring those together is app/'s job, and
// keeping it here stops shared/events from importing auth (which would invert the
// dependency ring).
type sseHandler struct {
	bus events.Bus
	// heartbeat overrides the ping interval; zero uses defaultHeartbeatInterval.
	// Present so tests can drive a short interval.
	heartbeat time.Duration
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

	// A caught-up reconnect (Last-Event-ID with nothing newer) and a
	// post-restart reconnect both make Replay return empty. Do NOT 204 in that
	// case — 204 tells the EventSource contract to stop, which turned every idle
	// reconnect into a hot-loop and kept a live stream from ever staying open
	// (F1). Replay whatever exists (possibly nothing) and fall through to
	// Subscribe so the stream is held open.
	if lastID := r.Header.Get("Last-Event-ID"); lastID != "" {
		if id, err := strconv.ParseUint(lastID, 10, 64); err == nil {
			replayed := h.bus.Replay(userId, id)
			if replayGapped(replayed, id) {
				// Events between the client's cursor and the retained tail were
				// evicted (F4). A partial replay would leave the client silently
				// stale, so tell it to fully reconcile instead of streaming a
				// hole-y history.
				fmt.Fprint(w, "event: resync\ndata: {}\n\n")
			} else {
				for _, evt := range replayed {
					writeSSEEvent(w, evt)
				}
			}
		}
	}

	ch, cancel := h.bus.Subscribe(userId)
	defer cancel()

	// Flush the response head + an initial comment immediately so the client's
	// onprogress fires (its watchdog starts) even before any real event, and so
	// the status is unambiguously 200.
	fmt.Fprint(w, ":ok\n\n")
	flusher.Flush()

	slog.InfoContext(r.Context(), "sse.connected", "user_id", userId.String())

	interval := h.heartbeat
	if interval <= 0 {
		interval = defaultHeartbeatInterval
	}
	heartbeat := time.NewTicker(interval)
	defer heartbeat.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			slog.InfoContext(r.Context(), "sse.disconnected", "user_id", userId.String())
			return
		case evt := <-ch:
			writeSSEEvent(w, evt)
			flusher.Flush()
		case <-heartbeat.C:
			fmt.Fprint(w, ":ping\n\n")
			flusher.Flush()
		}
	}
}

// replayGapped reports whether the replayed tail skips past the client's cursor
// — i.e. the oldest retained event is newer than afterID+1, so events in between
// were evicted from the ring and are unrecoverable for this client.
func replayGapped(replayed []events.Event, afterID uint64) bool {
	if len(replayed) == 0 {
		return false
	}
	return replayed[0].ID > afterID+1
}

func writeSSEEvent(w http.ResponseWriter, evt events.Event) {
	data, err := json.Marshal(evt.Payload)
	if err != nil {
		slog.Warn("sse.marshal_failed", "event_type", evt.Type, "error", err)
		return
	}
	fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", evt.ID, evt.Type, data)
}
