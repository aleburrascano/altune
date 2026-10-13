package handler

import (
	"altune/go-api/internal/eventstream/adapters/authbridge"
	"altune/go-api/internal/eventstream/domain"
	"altune/go-api/internal/eventstream/ports"
	"altune/go-api/internal/eventstream/service/connlimit"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/httputil"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

const (
	defaultHeartbeatInterval = 25 * time.Second
	defaultSSEWriteTimeout   = 10 * time.Second
	defaultMaxConnsPerUser   = 8
	defaultMaxConnsTotal     = 2048
)

type EventStreamHandler struct {
	bus          ports.EventSubscriber
	heartbeat    time.Duration
	writeTimeout time.Duration
	limiter      *connlimit.Limiter
	shutdown     <-chan struct{}
}

func NewEventStreamHandler(bus ports.EventSubscriber, maxConnsTotal int) *EventStreamHandler {
	if maxConnsTotal <= 0 {
		maxConnsTotal = defaultMaxConnsTotal
	}
	return &EventStreamHandler{
		bus:          bus,
		heartbeat:    defaultHeartbeatInterval,
		writeTimeout: defaultSSEWriteTimeout,
		limiter:      connlimit.New(defaultMaxConnsPerUser, maxConnsTotal),
	}
}

func (h *EventStreamHandler) WithShutdown(done <-chan struct{}) *EventStreamHandler {
	h.shutdown = done
	return h
}

func (h *EventStreamHandler) WithHeartbeat(d time.Duration) *EventStreamHandler {
	h.heartbeat = d
	return h
}

func (h *EventStreamHandler) WithWriteTimeout(d time.Duration) *EventStreamHandler {
	h.writeTimeout = d
	return h
}

func (h *EventStreamHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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

type replayOutcome struct {
	dedupThroughID     uint64
	deliveredThroughID uint64
}

func (h *EventStreamHandler) setup(
	w http.ResponseWriter,
	r *http.Request,
) (*sseConn, <-chan events.Event, func(), bool) {
	userId, ok := authbridge.RequireUserID(w, r)
	if !ok {
		return nil, nil, nil, false
	}
	if _, ok := w.(http.Flusher); !ok {
		httputil.WritePlainError(w, http.StatusInternalServerError, "streaming not supported")
		return nil, nil, nil, false
	}
	if !h.acquireSlot(w, r, userId) {
		return nil, nil, nil, false
	}

	httputil.ClearWriteDeadline(w)
	setSSEHeaders(w)
	conn := &sseConn{ctx: r.Context(), rc: http.NewResponseController(w), w: w, userId: userId, writeTimeout: h.writeTimeout}
	ch, cancel := h.bus.Subscribe(userId)
	return conn, ch, cancel, true
}

func (h *EventStreamHandler) replayHistory(conn *sseConn, r *http.Request) (replayOutcome, bool) {
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

func (h *EventStreamHandler) serveLive(
	r *http.Request,
	conn *sseConn,
	ch <-chan events.Event,
	replayed replayOutcome,
) {
	if err := conn.writeFrame(":ok\n\n"); err != nil {
		return
	}
	slog.InfoContext(r.Context(), "sse.connected", "user_id", conn.userId.String())

	ctx, cancel := authbridge.UntilTokenExpiry(r.Context())
	defer cancel()
	h.stream(ctx, conn, ch, replayed)
}

func (h *EventStreamHandler) acquireSlot(w http.ResponseWriter, r *http.Request, userId shared.UserId) bool {
	if h.limiter == nil {
		return true
	}
	err := h.limiter.Acquire(userId.String())
	if err == nil {
		return true
	}
	slog.WarnContext(r.Context(), connLimitLogEvent(err), "user_id", userId.String())
	httputil.WritePlainError(w, http.StatusTooManyRequests, "too many event streams")
	return false
}

func connLimitLogEvent(err error) string {
	if errors.Is(err, domain.ErrGlobalConnLimit) {
		return "sse.global_connection_limit"
	}
	return "sse.connection_limit"
}

func disconnectLogEvent(ctx context.Context) string {
	if errors.Is(context.Cause(ctx), authbridge.ErrTokenExpired) {
		return "sse.token_expired"
	}
	return "sse.disconnected"
}

func (h *EventStreamHandler) releaseSlot(userId shared.UserId) {
	if h.limiter != nil {
		h.limiter.Release(userId.String())
	}
}

func (h *EventStreamHandler) stream(ctx context.Context, conn *sseConn, ch <-chan events.Event, replayed replayOutcome) {
	heartbeat := time.NewTicker(h.heartbeatInterval())
	defer heartbeat.Stop()

	deliveredThroughID := h.startingID(conn, replayed)
	for {
		var keepGoing bool
		select {
		case <-ctx.Done():
			logDisconnect(ctx, conn, disconnectLogEvent(ctx))
		case <-h.shutdown:
			logDisconnect(ctx, conn, "sse.disconnected")
		case evt := <-ch:
			deliveredThroughID, keepGoing = h.deliverLive(conn, evt, replayed.dedupThroughID, deliveredThroughID)
		case <-heartbeat.C:
			deliveredThroughID, keepGoing = h.beat(conn, ch, deliveredThroughID)
		}
		if !keepGoing {
			return
		}
	}
}

func logDisconnect(ctx context.Context, conn *sseConn, event string) {
	slog.InfoContext(ctx, event, "user_id", conn.userId.String())
}

func (h *EventStreamHandler) heartbeatInterval() time.Duration {
	if h.heartbeat <= 0 {
		return defaultHeartbeatInterval
	}
	return h.heartbeat
}

func (h *EventStreamHandler) startingID(conn *sseConn, replayed replayOutcome) uint64 {
	if replayed.deliveredThroughID == 0 {
		return h.bus.LatestID(conn.userId)
	}
	return replayed.deliveredThroughID
}

func (h *EventStreamHandler) deliverLive(conn *sseConn, evt events.Event, dedupThroughID, deliveredThroughID uint64) (uint64, bool) {
	if evt.ID <= dedupThroughID {
		return deliveredThroughID, true
	}
	if err := h.writeLiveEvent(conn, evt, deliveredThroughID); err != nil {
		return deliveredThroughID, false
	}
	return evt.ID, true
}

func (h *EventStreamHandler) beat(conn *sseConn, ch <-chan events.Event, deliveredThroughID uint64) (uint64, bool) {
	through, err := h.writeHeartbeat(conn, ch, deliveredThroughID)
	return through, err == nil
}

func (h *EventStreamHandler) writeHeartbeat(conn *sseConn, ch <-chan events.Event, deliveredThroughID uint64) (uint64, error) {
	if err := conn.writeFrame(":ping\n\n"); err != nil {
		return deliveredThroughID, err
	}
	latestID := h.bus.LatestID(conn.userId)
	if len(ch) != 0 || latestID <= deliveredThroughID {
		return deliveredThroughID, nil
	}
	slog.WarnContext(conn.ctx, "sse.stream_gap_at_heartbeat",
		"user_id", conn.userId.String(), "delivered_through_id", deliveredThroughID,
		"latest_id", latestID)
	if err := conn.writeResync(); err != nil {
		return deliveredThroughID, err
	}
	return latestID, nil
}

func (h *EventStreamHandler) writeLiveEvent(conn *sseConn, evt events.Event, deliveredThroughID uint64) error {
	if streamGapped(deliveredThroughID, evt.ID) {
		slog.WarnContext(conn.ctx, "sse.stream_gap",
			"user_id", conn.userId.String(), "delivered_through_id", deliveredThroughID,
			"event_id", evt.ID, "lost", evt.ID-deliveredThroughID-1)
		if err := conn.writeResync(); err != nil {
			return err
		}
	}
	return conn.writeEvent(evt)
}

func (h *EventStreamHandler) resume(conn *sseConn, lastID string) (replayOutcome, error) {
	id, err := strconv.ParseUint(lastID, 10, 64)
	if err != nil {
		return replayOutcome{}, conn.writeResync()
	}
	return h.replay(conn, id)
}

func (h *EventStreamHandler) replay(conn *sseConn, afterID uint64) (replayOutcome, error) {
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

func (h *EventStreamHandler) resyncOutOfRange(conn *sseConn, afterID uint64) (replayOutcome, error) {
	slog.WarnContext(conn.ctx, "sse.last_event_id_out_of_range",
		"user_id", conn.userId.String(), "after_id", afterID, "highest_issued_id", h.bus.HighestIssuedID())
	return replayOutcome{}, conn.writeResync()
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
