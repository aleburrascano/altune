package handler

import (
	"altune/go-api/internal/observe/internal/shell/eventtap"
	"altune/go-api/internal/shared/httputil"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/requestuser"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

func (h *Handler) registerStreams(r chi.Router) {
	r.With(auditStreamOpen).Get("/events/stream", h.streamEvents)
	r.With(auditStreamOpen).Get("/logs/stream", h.streamLogs)
}

func auditStreamOpen(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slog.InfoContext(r.Context(), "observe.read",
			slog.String("actor", streamActor(r.Context())),
			slog.String(logging.KeyMethod, r.Method),
			slog.String(logging.KeyPath, r.URL.Path),
			logging.CorrelationAttr(r.Context()),
		)
		next.ServeHTTP(w, r)
	})
}

func streamActor(ctx context.Context) string {
	if id, known := requestuser.UserIDFromContext(ctx); known {
		return id.String()
	}
	return "unknown"
}

func (h *Handler) streamEvents(w http.ResponseWriter, r *http.Request) {
	if !h.hasLiveEventFeed() {
		httputil.HandleServiceError(w, r, errEventFeedUnavailable)
		return
	}
	ch, cancel, err := h.deps.Events.Subscribe()
	if err != nil {
		rejectSubscription(w, r, "events", err, eventtap.ErrTooManySubscribers)
		return
	}
	defer cancel()
	ctx, stop := h.untilShutdown(r.Context())
	defer stop()
	streamSSE(w, r.WithContext(ctx), ch, h.streams)
}

func (h *Handler) hasLiveEventFeed() bool {
	return h.deps.Events != nil && h.deps.Events.Available()
}

func (h *Handler) streamLogs(w http.ResponseWriter, r *http.Request) {
	if h.deps.Logs == nil {
		httputil.HandleServiceError(w, r, streamUnavailable("logs"))
		return
	}
	ch, cancel, err := h.deps.Logs.Subscribe()
	if err != nil {
		rejectSubscription(w, r, "logs", err, logging.ErrTooManySubscribers)
		return
	}
	defer cancel()
	ctx, stop := h.untilShutdown(r.Context())
	defer stop()
	streamSSE(w, r.WithContext(ctx), ch, h.streams)
}

type tapEventDTO struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	User      string    `json:"user,omitempty"`
	Subject   string    `json:"subject,omitempty"`
	CorrID    string    `json:"corr_id,omitempty"`
}

func projectTapEvent(ev eventtap.TapEvent) tapEventDTO {
	return tapEventDTO{
		Type:      ev.Type,
		Timestamp: ev.Timestamp,
		User:      userDigest(ev.User),
		Subject:   ev.Subject,
		CorrID:    ev.CorrID,
	}
}

const userDigestBytes = 4

func userDigest(user string) string {
	if user == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(user))
	return hex.EncodeToString(sum[:userDigestBytes])
}
