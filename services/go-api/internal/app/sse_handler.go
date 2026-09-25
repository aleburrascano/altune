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
	// defaultMaxConnsTotal is the server-wide ceiling on concurrent /v1/events
	// streams, applied when the configured SSE_MAX_CONNS is not positive.
	defaultMaxConnsTotal = 2048
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

// newSSEHandler builds the /v1/events handler. maxConnsTotal caps concurrent
// streams across all users; a non-positive value falls back to
// defaultMaxConnsTotal so a misconfiguration can never mean "unbounded".
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

// connLimiter caps the concurrent streams a single user may hold open
// (maxPerKey) and those held open across all users (maxTotal). A non-positive
// limit disables that bound. Both are checked and reserved under one lock, so
// parallel connects cannot overshoot either ceiling.
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

// acquire reserves a slot for key, or returns errUserConnLimit or
// errGlobalConnLimit without reserving anything when a ceiling is reached.
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

// release frees a slot previously reserved for key. Releasing a key that holds
// no slot is a no-op, so a stray release cannot drive the total negative.
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
	// Phase 1: authenticate, reserve a connection slot and subscribe.
	userId, rc, ch, cancel, ok := h.setup(w, r)
	if !ok {
		return
	}
	defer h.releaseSlot(userId)
	defer cancel()

	// Phase 2: replay any events the client missed since its Last-Event-ID.
	replayed, ok := h.replayHistory(rc, w, r, userId)
	if !ok {
		return
	}

	// Phase 3: serve the live stream with heartbeats.
	h.serveLive(r, rc, w, ch, userId, replayed)
}

// replayOutcome is what phase 2 leaves the live stream to work from. The two
// IDs differ whenever a resync stood in for the replay: the client still holds
// everything through dedupThroughID, but this handler delivered none of it, so
// deliveredThroughID is zero and the live stream has no baseline to call a gap.
type replayOutcome struct {
	dedupThroughID     uint64
	deliveredThroughID uint64
}

// setup authenticates the request, verifies streaming support, reserves a
// connection slot, writes the SSE headers and subscribes to the user's event
// channel. On success the caller owns the returned cancel func and must release
// the connection slot. Subscribe happens BEFORE any replay so an event
// published during replay lands on the live channel instead of the gap between
// snapshot and subscribe (#371); the overlap is deduped by ID in stream via
// lastReplayedID.
func (h *sseHandler) setup(
	w http.ResponseWriter,
	r *http.Request,
) (shared.UserId, *http.ResponseController, <-chan events.Event, func(), bool) {
	userId, ok := auth.RequireUserID(w, r)
	if !ok {
		return shared.UserId{}, nil, nil, nil, false
	}
	if _, ok := w.(http.Flusher); !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return shared.UserId{}, nil, nil, nil, false
	}
	if !h.acquireSlot(w, userId) {
		return shared.UserId{}, nil, nil, nil, false
	}

	// The stream outlives the route-level write deadline; writeFrame bounds
	// each frame with its own deadline instead.
	httputil.ClearWriteDeadline(w)
	setSSEHeaders(w)
	rc := http.NewResponseController(w)
	ch, cancel := h.bus.Subscribe(userId)
	return userId, rc, ch, cancel, true
}

// replayHistory replays events after the client's Last-Event-ID. The bool is
// false when the connection should be abandoned (a write failed mid-replay).
func (h *sseHandler) replayHistory(rc *http.ResponseController, w http.ResponseWriter, r *http.Request, userId shared.UserId) (replayOutcome, bool) {
	lastID := r.Header.Get("Last-Event-ID")
	if lastID == "" {
		return replayOutcome{}, true
	}
	replayed, err := h.resume(rc, w, userId, lastID)
	if err != nil {
		return replayOutcome{}, false
	}
	return replayed, true
}

// serveLive acknowledges the connection and then pumps the live event stream
// and heartbeats until the client disconnects or a write fails.
func (h *sseHandler) serveLive(
	r *http.Request,
	rc *http.ResponseController,
	w http.ResponseWriter,
	ch <-chan events.Event,
	userId shared.UserId,
	replayed replayOutcome,
) {
	if err := h.writeFrame(rc, w, ":ok\n\n"); err != nil {
		return
	}
	slog.InfoContext(r.Context(), "sse.connected", "user_id", userId.String())

	h.stream(r.Context(), rc, w, ch, userId, replayed)
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

// connLimitLogEvent names the rejection so operators can tell one noisy account
// (per-user cap) from server-wide saturation (global cap).
func connLimitLogEvent(err error) string {
	if errors.Is(err, errGlobalConnLimit) {
		return "sse.global_connection_limit"
	}
	return "sse.connection_limit"
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
	replayed replayOutcome,
) {
	interval := h.heartbeat
	if interval <= 0 {
		interval = defaultHeartbeatInterval
	}
	heartbeat := time.NewTicker(interval)
	defer heartbeat.Stop()

	deliveredThroughID := replayed.deliveredThroughID
	for {
		select {
		case <-ctx.Done():
			slog.InfoContext(ctx, "sse.disconnected", "user_id", userId.String())
			return
		case <-h.shutdown:
			slog.InfoContext(ctx, "sse.disconnected", "user_id", userId.String())
			return
		case evt := <-ch:
			if evt.ID <= replayed.dedupThroughID {
				continue // already delivered by replay; dedup the overlap window
			}
			if err := h.writeLiveEvent(rc, w, userId, evt, deliveredThroughID); err != nil {
				return
			}
			deliveredThroughID = evt.ID
		case <-heartbeat.C:
			if err := h.writeFrame(rc, w, ":ping\n\n"); err != nil {
				return
			}
		}
	}
}

// writeLiveEvent writes evt, preceded by a resync when the bus dropped the
// events between deliveredThroughID and it (#2016). A full subscriber channel
// leaves the connection healthy, so the client never reconnects and never
// replays: without the resync it holds stale state until it refetches by chance.
func (h *sseHandler) writeLiveEvent(rc *http.ResponseController, w http.ResponseWriter, userId shared.UserId, evt events.Event, deliveredThroughID uint64) error {
	if streamGapped(deliveredThroughID, evt.ID) {
		slog.Warn("sse.stream_gap",
			"user_id", userId.String(), "delivered_through_id", deliveredThroughID,
			"event_id", evt.ID, "lost", evt.ID-deliveredThroughID-1)
		if err := h.writeResync(rc, w); err != nil {
			return err
		}
	}
	return h.writeEvent(rc, w, evt)
}

// resume replays events after the client's Last-Event-ID. A malformed id cannot
// be parsed, so it signals resync rather than silently dropping to a live-only
// stream, mirroring the ring-buffer-gap case in replay.
func (h *sseHandler) resume(rc *http.ResponseController, w http.ResponseWriter, userId shared.UserId, lastID string) (replayOutcome, error) {
	id, err := strconv.ParseUint(lastID, 10, 64)
	if err != nil {
		return replayOutcome{}, h.writeResync(rc, w)
	}
	return h.replay(rc, w, userId, id)
}

func (h *sseHandler) replay(rc *http.ResponseController, w http.ResponseWriter, userId shared.UserId, afterID uint64) (replayOutcome, error) {
	if afterID > h.bus.HighestIssuedID() {
		return h.resyncOutOfRange(rc, w, userId, afterID)
	}
	replayed := h.bus.Replay(userId, afterID)
	if replayGapped(replayed, afterID) {
		return replayOutcome{dedupThroughID: afterID}, h.writeResync(rc, w)
	}
	outcome := replayOutcome{dedupThroughID: afterID, deliveredThroughID: afterID}
	for _, evt := range replayed {
		if err := h.writeEvent(rc, w, evt); err != nil {
			return outcome, err
		}
		outcome = replayOutcome{dedupThroughID: evt.ID, deliveredThroughID: evt.ID}
	}
	return outcome, nil
}

// resyncOutOfRange handles a Last-Event-ID this process never issued (#1013).
// Replaying from it would come back empty and look "caught up", and deduping
// live events against it would drop every one of them for the life of the
// connection. Signal a resync instead and dedup nothing: after a resync any
// overlap with the refetched state is harmless, a starved stream is not.
func (h *sseHandler) resyncOutOfRange(rc *http.ResponseController, w http.ResponseWriter, userId shared.UserId, afterID uint64) (replayOutcome, error) {
	slog.Warn("sse.last_event_id_out_of_range",
		"user_id", userId.String(), "after_id", afterID, "highest_issued_id", h.bus.HighestIssuedID())
	return replayOutcome{}, h.writeResync(rc, w)
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

// streamGapped reports whether the bus dropped events between the last one this
// connection delivered and the next to arrive. Per-user IDs are consecutive, so
// any jump is a drop. A zero baseline means nothing has been delivered yet and
// the first live ID is arbitrary: there is no gap to see.
func streamGapped(deliveredThroughID, nextID uint64) bool {
	if deliveredThroughID == 0 {
		return false
	}
	return nextID > deliveredThroughID+1
}
