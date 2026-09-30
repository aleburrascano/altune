package app

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/httputil"
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
	defaultMaxConnsTotal     = 2048
)

var (
	errUserConnLimit   = errors.New("sse: per-user connection limit reached")
	errGlobalConnLimit = errors.New("sse: global connection limit reached")
)

type sseHandler struct {
	bus          events.Subscriber
	heartbeat    time.Duration
	writeTimeout time.Duration
	limiter      *connLimiter
	shutdown     <-chan struct{}
}

func newSSEHandler(bus events.Subscriber, maxConnsTotal int) *sseHandler {
	if maxConnsTotal <= 0 {
		maxConnsTotal = defaultMaxConnsTotal
	}
	return &sseHandler{
		bus:          bus,
		heartbeat:    defaultHeartbeatInterval,
		writeTimeout: defaultSSEWriteTimeout,
		limiter:      newConnLimiter(defaultMaxConnsPerUser, maxConnsTotal),
	}
}

func (h *sseHandler) withShutdown(done <-chan struct{}) *sseHandler {
	h.shutdown = done
	return h
}

type connLimiter struct {
	mu        sync.Mutex
	maxPerKey int
	maxTotal  int
	total     int
	count     map[string]int
}

func newConnLimiter(maxPerKey, maxTotal int) *connLimiter {
	return &connLimiter{maxPerKey: maxPerKey, maxTotal: maxTotal, count: make(map[string]int)}
}

func (l *connLimiter) acquire(key string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.maxPerKey > 0 && l.count[key] >= l.maxPerKey {
		return errUserConnLimit
	}
	if l.maxTotal > 0 && l.total >= l.maxTotal {
		return errGlobalConnLimit
	}
	l.count[key]++
	l.total++
	return nil
}

func (l *connLimiter) release(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	n, ok := l.count[key]
	if !ok {
		return
	}
	l.total--
	if n <= 1 {
		delete(l.count, key)
		return
	}
	l.count[key] = n - 1
}

func (h *sseHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, ch, cancel, ok := h.setup(w, r)
	if !ok {
		return
	}
	defer h.releaseSlot(conn.userId)
	defer cancel()

	replayed, ok := h.replayHistory(conn, r)
	if !ok {
		return
	}

	h.serveLive(r, conn, ch, replayed)
}

type sseConn struct {
	rc           *http.ResponseController
	w            http.ResponseWriter
	userId       shared.UserId
	writeTimeout time.Duration
}

type replayOutcome struct {
	dedupThroughID     uint64
	deliveredThroughID uint64
}

func (h *sseHandler) setup(
	w http.ResponseWriter,
	r *http.Request,
) (*sseConn, <-chan events.Event, func(), bool) {
	userId, ok := auth.RequireUserID(w, r)
	if !ok {
		return nil, nil, nil, false
	}
	if _, ok := w.(http.Flusher); !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return nil, nil, nil, false
	}
	if !h.acquireSlot(w, userId) {
		return nil, nil, nil, false
	}

	httputil.ClearWriteDeadline(w)
	setSSEHeaders(w)
	conn := &sseConn{rc: http.NewResponseController(w), w: w, userId: userId, writeTimeout: h.writeTimeout}
	ch, cancel := h.bus.Subscribe(userId)
	return conn, ch, cancel, true
}

func (h *sseHandler) replayHistory(conn *sseConn, r *http.Request) (replayOutcome, bool) {
	lastID := r.Header.Get("Last-Event-ID")
	if lastID == "" {
		return replayOutcome{}, true
	}
	replayed, err := h.resume(conn, lastID)
	if err != nil {
		return replayOutcome{}, false
	}
	return replayed, true
}

func (h *sseHandler) serveLive(
	r *http.Request,
	conn *sseConn,
	ch <-chan events.Event,
	replayed replayOutcome,
) {
	if err := conn.writeFrame(":ok\n\n"); err != nil {
		return
	}
	slog.InfoContext(r.Context(), "sse.connected", "user_id", conn.userId.String())

	ctx, cancel := auth.UntilTokenExpiry(r.Context())
	defer cancel()
	h.stream(ctx, conn, ch, replayed)
}

func (h *sseHandler) acquireSlot(w http.ResponseWriter, userId shared.UserId) bool {
	if h.limiter == nil {
		return true
	}
	err := h.limiter.acquire(userId.String())
	if err == nil {
		return true
	}
	slog.Warn(connLimitLogEvent(err), "user_id", userId.String())
	http.Error(w, "too many event streams", http.StatusTooManyRequests)
	return false
}

func connLimitLogEvent(err error) string {
	if errors.Is(err, errGlobalConnLimit) {
		return "sse.global_connection_limit"
	}
	return "sse.connection_limit"
}

func disconnectLogEvent(ctx context.Context) string {
	if errors.Is(context.Cause(ctx), auth.ErrTokenExpired) {
		return "sse.token_expired"
	}
	return "sse.disconnected"
}

func (h *sseHandler) releaseSlot(userId shared.UserId) {
	if h.limiter != nil {
		h.limiter.release(userId.String())
	}
}

func (h *sseHandler) stream(
	ctx context.Context,
	conn *sseConn,
	ch <-chan events.Event,
	replayed replayOutcome,
) {
	interval := h.heartbeat
	if interval <= 0 {
		interval = defaultHeartbeatInterval
	}
	heartbeat := time.NewTicker(interval)
	defer heartbeat.Stop()

	deliveredThroughID := replayed.deliveredThroughID
	if deliveredThroughID == 0 {
		deliveredThroughID = h.bus.LatestID(conn.userId)
	}
	for {
		select {
		case <-ctx.Done():
			slog.InfoContext(ctx, disconnectLogEvent(ctx), "user_id", conn.userId.String())
			return
		case <-h.shutdown:
			slog.InfoContext(ctx, "sse.disconnected", "user_id", conn.userId.String())
			return
		case evt := <-ch:
			if evt.ID <= replayed.dedupThroughID {
				continue
			}
			if err := h.writeLiveEvent(conn, evt, deliveredThroughID); err != nil {
				return
			}
			deliveredThroughID = evt.ID
		case <-heartbeat.C:
			if err := conn.writeFrame(":ping\n\n"); err != nil {
				return
			}
			latestID := h.bus.LatestID(conn.userId)
			if len(ch) == 0 && latestID > deliveredThroughID {
				slog.Warn("sse.stream_gap_at_heartbeat",
					"user_id", conn.userId.String(), "delivered_through_id", deliveredThroughID,
					"latest_id", latestID)
				if err := conn.writeResync(); err != nil {
					return
				}
				deliveredThroughID = latestID
			}
		}
	}
}

func (h *sseHandler) writeLiveEvent(conn *sseConn, evt events.Event, deliveredThroughID uint64) error {
	if streamGapped(deliveredThroughID, evt.ID) {
		slog.Warn("sse.stream_gap",
			"user_id", conn.userId.String(), "delivered_through_id", deliveredThroughID,
			"event_id", evt.ID, "lost", evt.ID-deliveredThroughID-1)
		if err := conn.writeResync(); err != nil {
			return err
		}
	}
	return conn.writeEvent(evt)
}

func (h *sseHandler) resume(conn *sseConn, lastID string) (replayOutcome, error) {
	id, err := strconv.ParseUint(lastID, 10, 64)
	if err != nil {
		return replayOutcome{}, conn.writeResync()
	}
	return h.replay(conn, id)
}

func (h *sseHandler) replay(conn *sseConn, afterID uint64) (replayOutcome, error) {
	if afterID > h.bus.HighestIssuedID() {
		return h.resyncOutOfRange(conn, afterID)
	}
	replayed := h.bus.Replay(conn.userId, afterID)
	if h.bus.ResumeGapped(conn.userId, afterID) || replayGapped(replayed, afterID) {
		return replayOutcome{dedupThroughID: afterID}, conn.writeResync()
	}
	outcome := replayOutcome{dedupThroughID: afterID, deliveredThroughID: afterID}
	for _, evt := range replayed {
		if err := conn.writeEvent(evt); err != nil {
			return outcome, err
		}
		outcome = replayOutcome{dedupThroughID: evt.ID, deliveredThroughID: evt.ID}
	}
	return outcome, nil
}

func (h *sseHandler) resyncOutOfRange(conn *sseConn, afterID uint64) (replayOutcome, error) {
	slog.Warn("sse.last_event_id_out_of_range",
		"user_id", conn.userId.String(), "after_id", afterID, "highest_issued_id", h.bus.HighestIssuedID())
	return replayOutcome{}, conn.writeResync()
}

func (c *sseConn) writeFrame(frame string) error {
	if c.writeTimeout > 0 {
		if err := c.rc.SetWriteDeadline(time.Now().Add(c.writeTimeout)); err != nil && !errors.Is(err, http.ErrNotSupported) {
			return err
		}
	}
	if _, err := io.WriteString(c.w, frame); err != nil {
		return err
	}
	return c.rc.Flush()
}

func (c *sseConn) writeEvent(evt events.Event) error {
	data, err := json.Marshal(evt.Payload)
	if err != nil {
		slog.Warn("sse.marshal_failed", "event_type", evt.Type, "error", err)
		return c.writeResync()
	}
	return c.writeFrame(fmt.Sprintf("id: %d\nevent: %s\ndata: %s\n\n", evt.ID, evt.Type, data))
}

func (c *sseConn) writeResync() error {
	return c.writeFrame("event: resync\ndata: {}\n\n")
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

func streamGapped(deliveredThroughID, nextID uint64) bool {
	if deliveredThroughID == 0 {
		return false
	}
	return nextID > deliveredThroughID+1
}
