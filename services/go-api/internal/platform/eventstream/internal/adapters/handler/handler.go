package handler

import (
	"altune/go-api/internal/platform/eventstream/internal/domain"
	"altune/go-api/internal/platform/eventstream/internal/ports"
	apierror "altune/go-api/internal/shared/apierror"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/httpserver"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/requestuser"
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
	now          clock.Func
	newTicker    clock.TickerFunc
	bus          ports.EventSubscriber
	heartbeat    time.Duration
	writeTimeout time.Duration
	limiter      *domain.Limiter
	shutdown     <-chan struct{}
}

type Config struct {
	Now           clock.Func
	NewTicker     clock.TickerFunc
	Bus           ports.EventSubscriber
	MaxConnsTotal int
	Heartbeat     time.Duration
	WriteTimeout  time.Duration
	Shutdown      <-chan struct{}
}

func NewEventStreamHandler(cfg Config) *EventStreamHandler {
	if cfg.Now == nil {
		panic("handler.NewEventStreamHandler: a nil clock")
	}
	if cfg.NewTicker == nil {
		panic("handler.NewEventStreamHandler: a nil ticker")
	}
	if cfg.MaxConnsTotal <= 0 {
		cfg.MaxConnsTotal = defaultMaxConnsTotal
	}
	if cfg.Heartbeat <= 0 {
		cfg.Heartbeat = defaultHeartbeatInterval
	}
	if cfg.WriteTimeout <= 0 {
		cfg.WriteTimeout = defaultSSEWriteTimeout
	}
	return &EventStreamHandler{
		now:          cfg.Now,
		newTicker:    cfg.NewTicker,
		bus:          cfg.Bus,
		heartbeat:    cfg.Heartbeat,
		writeTimeout: cfg.WriteTimeout,
		limiter:      domain.NewLimiter(defaultMaxConnsPerUser, cfg.MaxConnsTotal),
		shutdown:     cfg.Shutdown,
	}
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
	userId := requestuser.UserID(r)
	if _, ok := w.(http.Flusher); !ok {
		httpserver.HandleServiceError(w, r, apierror.NewError(apierror.CodeEventStreamUnsupported, "streaming not supported"))
		return nil, nil, nil, false
	}
	if !h.acquireSlot(w, r, userId) {
		return nil, nil, nil, false
	}

	httpserver.ClearWriteDeadline(w)
	setSSEHeaders(w)
	conn := &sseConn{rc: http.NewResponseController(w), w: w, userId: userId, now: h.now, writeTimeout: h.writeTimeout}
	ch, cancel := h.bus.Subscribe(userId)
	return conn, ch, cancel, true
}

func (h *EventStreamHandler) replayHistory(conn *sseConn, r *http.Request) (replayOutcome, bool) {
	lastID := r.Header.Get("Last-Event-ID")
	if lastID == "" {
		return replayOutcome{}, true
	}
	replayed, err := h.resume(r.Context(), conn, lastID)
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
	slog.InfoContext(r.Context(), "sse.connected")

	ctx, cancel := requestuser.UntilTokenExpiry(r.Context())
	defer cancel()
	h.stream(ctx, conn, ch, replayed)
}

func (h *EventStreamHandler) acquireSlot(w http.ResponseWriter, r *http.Request, userId ids.UserId) bool {
	err := h.limiter.Acquire(userId.String())
	if err == nil {
		return true
	}
	slog.WarnContext(r.Context(), connLimitLogEvent(err))
	httpserver.HandleServiceError(w, r, apierror.NewError(apierror.CodeEventStreamTooManyStreams, "too many event streams"))
	return false
}

func disconnectLogEvent(ctx context.Context) string {
	if errors.Is(context.Cause(ctx), requestuser.ErrTokenExpired) {
		return "sse.token_expired"
	}
	return "sse.disconnected"
}

func (h *EventStreamHandler) releaseSlot(userId ids.UserId) {
	h.limiter.Release(userId.String())
}

func (h *EventStreamHandler) stream(ctx context.Context, conn *sseConn, ch <-chan events.Event, replayed replayOutcome) {
	beats, stopBeats := h.newTicker(h.heartbeatInterval())
	defer stopBeats()

	deliveredThroughID := h.startingID(conn, replayed)
	for {
		var keepGoing bool
		select {
		case <-ctx.Done():
			logDisconnect(ctx, conn, disconnectLogEvent(ctx))
		case <-h.shutdown:
			logDisconnect(ctx, conn, "sse.disconnected")
		case evt := <-ch:
			deliveredThroughID, keepGoing = h.deliverLive(ctx, conn, evt, replayed.dedupThroughID, deliveredThroughID)
		case <-beats:
			deliveredThroughID, keepGoing = h.beat(ctx, conn, ch, deliveredThroughID)
		}
		if !keepGoing {
			return
		}
	}
}

func logDisconnect(ctx context.Context, conn *sseConn, event string) {
	slog.InfoContext(ctx, event)
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

func (h *EventStreamHandler) deliverLive(ctx context.Context, conn *sseConn, evt events.Event, dedupThroughID, deliveredThroughID uint64) (uint64, bool) {
	if evt.ID <= dedupThroughID {
		return deliveredThroughID, true
	}
	if err := h.writeLiveEvent(ctx, conn, evt, deliveredThroughID); err != nil {
		return deliveredThroughID, false
	}
	return evt.ID, true
}

func (h *EventStreamHandler) beat(ctx context.Context, conn *sseConn, ch <-chan events.Event, deliveredThroughID uint64) (uint64, bool) {
	through, err := h.writeHeartbeat(ctx, conn, ch, deliveredThroughID)
	return through, err == nil
}

func (h *EventStreamHandler) writeHeartbeat(ctx context.Context, conn *sseConn, ch <-chan events.Event, deliveredThroughID uint64) (uint64, error) {
	if err := conn.writeFrame(":ping\n\n"); err != nil {
		return deliveredThroughID, err
	}
	latestID := h.bus.LatestID(conn.userId)
	if len(ch) != 0 || latestID <= deliveredThroughID {
		return deliveredThroughID, nil
	}
	slog.WarnContext(ctx, "sse.stream_gap_at_heartbeat",
		"delivered_through_id", deliveredThroughID,
		"latest_id", latestID)
	if err := conn.writeResync(); err != nil {
		return deliveredThroughID, err
	}
	return latestID, nil
}

func (h *EventStreamHandler) writeLiveEvent(ctx context.Context, conn *sseConn, evt events.Event, deliveredThroughID uint64) error {
	if streamGapped(deliveredThroughID, evt.ID) {
		slog.WarnContext(ctx, "sse.stream_gap",
			"delivered_through_id", deliveredThroughID,
			"event_id", evt.ID, "lost", evt.ID-deliveredThroughID-1)
		if err := conn.writeResync(); err != nil {
			return err
		}
	}
	return conn.writeEvent(ctx, evt)
}

func (h *EventStreamHandler) resume(ctx context.Context, conn *sseConn, lastID string) (replayOutcome, error) {
	id, err := strconv.ParseUint(lastID, 10, 64)
	if err != nil {
		return replayOutcome{}, conn.writeResync()
	}
	return h.replay(ctx, conn, id)
}

func (h *EventStreamHandler) replay(ctx context.Context, conn *sseConn, afterID uint64) (replayOutcome, error) {
	if afterID > h.bus.HighestIssuedID() {
		return h.resyncOutOfRange(ctx, conn, afterID)
	}
	replayed := h.bus.Replay(ctx, conn.userId, afterID)
	if h.bus.ResumeGapped(conn.userId, afterID) || replayGapped(replayed, afterID) {
		return replayOutcome{dedupThroughID: afterID}, conn.writeResync()
	}
	outcome := replayOutcome{dedupThroughID: afterID, deliveredThroughID: afterID}
	for _, evt := range replayed {
		if err := conn.writeEvent(ctx, evt); err != nil {
			return outcome, err
		}
		outcome = replayOutcome{dedupThroughID: evt.ID, deliveredThroughID: evt.ID}
	}
	return outcome, nil
}

func (h *EventStreamHandler) resyncOutOfRange(ctx context.Context, conn *sseConn, afterID uint64) (replayOutcome, error) {
	slog.WarnContext(ctx, "sse.last_event_id_out_of_range",
		"after_id", afterID, "highest_issued_id", h.bus.HighestIssuedID())
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
