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

const defaultHeartbeatInterval = 25 * time.Second

type sseHandler struct {
	bus       events.Subscriber
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

	if lastID := r.Header.Get("Last-Event-ID"); lastID != "" {
		if id, err := strconv.ParseUint(lastID, 10, 64); err == nil {
			replayed := h.bus.Replay(userId, id)
			if replayGapped(replayed, id) {
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
