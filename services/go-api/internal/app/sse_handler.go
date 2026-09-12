package app

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/events"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const (
	defaultHeartbeatInterval = 25 * time.Second
	defaultSSEWriteTimeout   = 10 * time.Second
	defaultMaxConnsPerUser   = 8
)

type sseHandler struct {
	bus          events.Subscriber
	heartbeat    time.Duration
	writeTimeout time.Duration
	limiter      *connLimiter
}

func newSSEHandler(bus events.Subscriber) *sseHandler {
	return &sseHandler{
		bus:          bus,
		heartbeat:    defaultHeartbeatInterval,
		writeTimeout: defaultSSEWriteTimeout,
		limiter:      newConnLimiter(defaultMaxConnsPerUser),
	}
}

// connLimiter caps the number of concurrent streams a single user may hold open.
type connLimiter struct {
	mu    sync.Mutex
	max   int
	count map[string]int
}

func newConnLimiter(limit int) *connLimiter {
	return &connLimiter{max: limit, count: make(map[string]int)}
}

func (l *connLimiter) acquire(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.max > 0 && l.count[key] >= l.max {
		return false
	}
	l.count[key]++
	return true
}

func (l *connLimiter) release(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.count[key] <= 1 {
		delete(l.count, key)
		return
	}
	l.count[key]--
}

func (h *sseHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	userId, ok := auth.RequireUserID(w, r)
	if !ok {
		return
	}
	if _, ok := w.(http.Flusher); !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}
	if !h.acquireSlot(w, userId) {
		return
	}
	defer h.releaseSlot(userId)

	setSSEHeaders(w)
	rc := http.NewResponseController(w)

	if lastID := r.Header.Get("Last-Event-ID"); lastID != "" {
		if id, err := strconv.ParseUint(lastID, 10, 64); err == nil {
			if err := h.replay(rc, w, userId, id); err != nil {
				return
			}
		}
	}

	ch, cancel := h.bus.Subscribe(userId)
	defer cancel()

	if err := h.writeFrame(rc, w, ":ok\n\n"); err != nil {
		return
	}
	slog.InfoContext(r.Context(), "sse.connected", "user_id", userId.String())

	h.stream(r.Context(), rc, w, ch, userId)
}

func (h *sseHandler) acquireSlot(w http.ResponseWriter, userId shared.UserId) bool {
	if h.limiter == nil {
		return true
	}
	if h.limiter.acquire(userId.String()) {
		return true
	}
	slog.Warn("sse.connection_limit", "user_id", userId.String())
	http.Error(w, "too many event streams", http.StatusTooManyRequests)
	return false
}

func (h *sseHandler) releaseSlot(userId shared.UserId) {
	if h.limiter != nil {
		h.limiter.release(userId.String())
	}
}

func (h *sseHandler) stream(
	ctx context.Context,
	rc *http.ResponseController,
	w http.ResponseWriter,
	ch <-chan events.Event,
	userId shared.UserId,
) {
	interval := h.heartbeat
	if interval <= 0 {
		interval = defaultHeartbeatInterval
	}
	heartbeat := time.NewTicker(interval)
	defer heartbeat.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.InfoContext(ctx, "sse.disconnected", "user_id", userId.String())
			return
		case evt := <-ch:
			if err := h.writeEvent(rc, w, evt); err != nil {
				return
			}
		case <-heartbeat.C:
			if err := h.writeFrame(rc, w, ":ping\n\n"); err != nil {
				return
			}
		}
	}
}

func (h *sseHandler) replay(rc *http.ResponseController, w http.ResponseWriter, userId shared.UserId, afterID uint64) error {
	replayed := h.bus.Replay(userId, afterID)
	if replayGapped(replayed, afterID) {
		return h.writeResync(rc, w)
	}
	for _, evt := range replayed {
		if err := h.writeEvent(rc, w, evt); err != nil {
			return err
		}
	}
	return nil
}

// writeFrame sets a per-write deadline so a client that has stopped reading
// cannot block the handler goroutine on Write/Flush forever, then flushes.
func (h *sseHandler) writeFrame(rc *http.ResponseController, w http.ResponseWriter, frame string) error {
	if h.writeTimeout > 0 {
		if err := rc.SetWriteDeadline(time.Now().Add(h.writeTimeout)); err != nil && !errors.Is(err, http.ErrNotSupported) {
			return err
		}
	}
	if _, err := io.WriteString(w, frame); err != nil {
		return err
	}
	return rc.Flush()
}

func (h *sseHandler) writeEvent(rc *http.ResponseController, w http.ResponseWriter, evt events.Event) error {
	data, err := json.Marshal(evt.Payload)
	if err != nil {
		// The event exists but cannot be serialized: signal a resync rather than
		// dropping it silently, which would leave the client permanently unaware.
		slog.Warn("sse.marshal_failed", "event_type", evt.Type, "error", err)
		return h.writeResync(rc, w)
	}
	return h.writeFrame(rc, w, fmt.Sprintf("id: %d\nevent: %s\ndata: %s\n\n", evt.ID, evt.Type, data))
}

// writeResync tells the client its view may be stale and it should refetch,
// the same signal replay emits when the ring buffer has gapped.
func (h *sseHandler) writeResync(rc *http.ResponseController, w http.ResponseWriter) error {
	return h.writeFrame(rc, w, "event: resync\ndata: {}\n\n")
}

func setSSEHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
}

func replayGapped(replayed []events.Event, afterID uint64) bool {
	if len(replayed) == 0 {
		return false
	}
	return replayed[0].ID > afterID+1
}
