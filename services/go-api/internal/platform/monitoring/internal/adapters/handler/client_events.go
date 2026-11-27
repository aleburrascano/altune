package handler

import (
	"altune/go-api/internal/platform/monitoring/internal/core/domain"
	"altune/go-api/internal/shared/apierror"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/httpserver"
	"altune/go-api/internal/shared/requestuser"
	"altune/go-api/internal/shared/telemetry"
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

const (
	maxClientEventBodyBytes  = 32 << 10
	maxCorrelationIDLength   = 64
	defaultClientEventBudget = 300
)

var errClientEventsRateLimited = apierror.NewError(apierror.CodeDiscoveryRateLimited, "too many requests, try again later")

type ClientEventRecorder interface {
	Record(ctx context.Context, event domain.ClientEvent) error
}

type ClientEventSink interface {
	Emit(ctx context.Context, event telemetry.ClientEvent)
}

type ClientEventLimit struct {
	Max    int
	Window time.Duration
}

var DefaultClientEventLimit = ClientEventLimit{Max: defaultClientEventBudget, Window: time.Minute}

type ClientEventHandler struct {
	recorder ClientEventRecorder
	sink     ClientEventSink
	limiter  func(http.Handler) http.Handler
}

func NewClientEventHandler(recorder ClientEventRecorder, sink ClientEventSink, limit ClientEventLimit, now clock.Func) *ClientEventHandler {
	if recorder == nil || sink == nil || now == nil || limit.Max <= 0 || limit.Window <= 0 {
		panic("handler.NewClientEventHandler: a recorder, a sink, a clock and a positive limit are required")
	}
	rate := httpserver.RateLimit{Every: limit.Window / time.Duration(limit.Max), Burst: limit.Max}
	return &ClientEventHandler{
		recorder: recorder,
		sink:     sink,
		limiter:  requestuser.LimitPerUser(rate, now, errClientEventsRateLimited, nil),
	}
}

func (h *ClientEventHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.With(h.limiter, httpserver.MaxBodySize(maxClientEventBodyBytes)).Post("/events", h.handleRecord)
	return r
}

type clientEventRequest struct {
	Type             string         `json:"type"`
	EventID          string         `json:"event_id"`
	CorrelationID    string         `json:"correlation_id"`
	ClientOccurredAt string         `json:"client_occurred_at"`
	Payload          map[string]any `json:"payload"`
	IgnoredSearchID  string         `json:"search_id"`
	IgnoredQueryNorm string         `json:"query_norm"`
}

func (h *ClientEventHandler) handleRecord(w http.ResponseWriter, r *http.Request) {
	userId := requestuser.UserID(r)

	var req clientEventRequest
	if !httpserver.DecodeJSONWithCode(w, r, &req, apierror.CodeDiscoveryInvalidBody) {
		return
	}
	eventType, ok := domain.ParseClientEventType(req.Type)
	if !ok {
		httpserver.HandleServiceError(w, r, apierror.NewError(apierror.CodeDiscoveryInvalidEventType, "invalid event type"))
		return
	}

	event := domain.ClientEvent{
		UserId:           userId,
		Type:             eventType,
		EventId:          req.EventID,
		CorrelationId:    boundedCorrelationID(req.CorrelationID),
		ClientOccurredAt: parseClientTime(req.ClientOccurredAt),
		Payload:          req.Payload,
	}
	if err := h.recorder.Record(r.Context(), event); err != nil {
		httpserver.HandleServiceError(w, r, err)
		return
	}

	h.sink.Emit(r.Context(), telemetry.ClientEvent{
		Type:          string(eventType),
		EventID:       req.EventID,
		CorrelationID: event.CorrelationId,
		AppVersion:    r.Header.Get(telemetry.AppVersionHeader),
		Platform:      r.Header.Get(telemetry.PlatformHeader),
		UserID:        userId.String(),
		Payload:       req.Payload,
	})
	w.WriteHeader(http.StatusNoContent)
}

func boundedCorrelationID(raw string) string {
	if len(raw) > maxCorrelationIDLength || strings.ContainsRune(raw, 0) {
		return ""
	}
	return raw
}

func parseClientTime(raw string) time.Time {
	if raw == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}
	}
	return t
}
