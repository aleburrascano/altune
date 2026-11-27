package handler

import (
	audioservice "altune/go-api/internal/library/internal/core/audio"
	"altune/go-api/internal/library/internal/domain"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/httpserver"
	"altune/go-api/internal/shared/requestuser"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

const maxAudioURLBatch = 200

type AudioURLHandler struct {
	svc             *audioservice.AudioLinkService
	prefetchEnabled bool
	now             clock.Func
	limiter         rateLimitMiddleware
}

func NewAudioURLHandler(svc *audioservice.AudioLinkService, now clock.Func, opts ...func(*AudioURLHandler)) (*AudioURLHandler, error) {
	if now == nil {
		return nil, fmt.Errorf("NewAudioURLHandler: %w", ErrClockRequired)
	}
	h := &AudioURLHandler{svc: svc, prefetchEnabled: true, now: now}
	for _, opt := range opts {
		opt(h)
	}
	h.limiter = newAudioRateLimiter(audioURLRateLimit, h.now)
	return h, nil
}

func MustNewAudioURLHandler(svc *audioservice.AudioLinkService, now clock.Func, opts ...func(*AudioURLHandler)) *AudioURLHandler {
	h, err := NewAudioURLHandler(svc, now, opts...)
	if err != nil {
		panic(err)
	}
	return h
}

func WithPrefetchEnabled(enabled bool) func(*AudioURLHandler) {
	return func(h *AudioURLHandler) {
		h.prefetchEnabled = enabled
	}
}

func (h *AudioURLHandler) Routes(r chi.Router) {
	r.With(h.limiter).Post("/audio-urls", h.HandleResolve)
}

type resolveAudioURLsRequest struct {
	TrackIDs []string `json:"track_ids"`
}

type audioURLDTO struct {
	TrackID   string `json:"track_id"`
	URL       string `json:"url"`
	Version   string `json:"version"`
	ExpiresAt string `json:"expires_at"`
}

type resolveAudioURLsResponse struct {
	URLs            []audioURLDTO `json:"urls"`
	PrefetchEnabled bool          `json:"prefetch_enabled"`
}

func (h *AudioURLHandler) HandleResolve(w http.ResponseWriter, r *http.Request) {
	start := h.now()
	userId := requestuser.UserID(r)

	var body resolveAudioURLsRequest
	if !httpserver.DecodeJSON(w, r, &body) {
		return
	}
	if len(body.TrackIDs) > maxAudioURLBatch {
		httpserver.HandleServiceError(w, r, domain.ErrBatchTooLarge)
		return
	}

	ids := make([]domain.TrackId, 0, len(body.TrackIDs))
	for _, raw := range body.TrackIDs {
		id, err := domain.ParseTrackId(raw)
		if err != nil {
			httpserver.HandleServiceError(w, r, domain.ErrInvalidTrackID)
			return
		}
		ids = append(ids, id)
	}

	resolved, err := h.svc.Resolve(r.Context(), userId, ids)
	if err != nil {
		httpserver.HandleServiceError(w, r, err)
		return
	}

	urls := make([]audioURLDTO, 0, len(resolved))
	for _, ru := range resolved {
		urls = append(urls, audioURLDTO{
			TrackID:   ru.TrackID.String(),
			URL:       ru.URL,
			Version:   ru.Version,
			ExpiresAt: ru.ExpiresAt.Format(time.RFC3339),
		})
	}
	slog.InfoContext(r.Context(), "audio_urls.handled",
		"requested", len(ids),
		"resolved", len(urls),
		"duration_ms", h.now().Sub(start).Milliseconds(),
	)
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	httpserver.WriteJSON(w, http.StatusOK, resolveAudioURLsResponse{URLs: urls, PrefetchEnabled: h.prefetchEnabled})
}
