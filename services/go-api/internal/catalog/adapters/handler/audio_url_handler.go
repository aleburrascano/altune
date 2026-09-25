package handler

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/service"
	"altune/go-api/internal/shared/httputil"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

const maxAudioURLBatch = 200

type AudioURLHandler struct {
	svc             *service.AudioURLService
	prefetchEnabled bool
	rateLimit       AudioRateLimit
	now             func() time.Time
	limiter         *audioRateLimiter
}

func NewAudioURLHandler(svc *service.AudioURLService, opts ...func(*AudioURLHandler)) *AudioURLHandler {
	h := &AudioURLHandler{svc: svc, prefetchEnabled: true, rateLimit: DefaultAudioURLRateLimit, now: time.Now}
	for _, opt := range opts {
		opt(h)
	}
	h.limiter = newAudioRateLimiter(h.rateLimit, h.now)
	return h
}

// WithAudioURLRateLimit replaces DefaultAudioURLRateLimit.
func WithAudioURLRateLimit(limit AudioRateLimit) func(*AudioURLHandler) {
	return func(h *AudioURLHandler) { h.rateLimit = limit }
}

// withAudioURLClock injects the limiter's clock so tests can refill buckets
// without sleeping.
func withAudioURLClock(now func() time.Time) func(*AudioURLHandler) {
	return func(h *AudioURLHandler) { h.now = now }
}

// WithPrefetchEnabled sets the client prefetch kill switch reported on every
// response (AUDIO_PREFETCH_ENABLED). While it is false, clients stop
// prefetching audio to disk and stream instead.
func WithPrefetchEnabled(enabled bool) func(*AudioURLHandler) {
	return func(h *AudioURLHandler) {
		h.prefetchEnabled = enabled
	}
}

// Routes registers the audio-url endpoint on r. It registers directly onto the
// shared router rather than returning a mountable chi.Router: mounting /audio-urls
// would add a trailing-slash variant and change the route table, so this keeps
// the path byte-identical to its previous hand-wiring. The endpoint is
// throttled per user.
func (h *AudioURLHandler) Routes(r chi.Router) {
	r.With(h.limiter.middleware).Post("/audio-urls", h.HandleResolve)
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
	start := time.Now()
	userId, ok := auth.RequireUserID(w, r)
	if !ok {
		return
	}

	var body resolveAudioURLsRequest
	if !httputil.DecodeJSON(w, r, &body) {
		return
	}
	if len(body.TrackIDs) > maxAudioURLBatch {
		httputil.HandleServiceError(w, r, domain.ErrBatchTooLarge)
		return
	}

	ids := make([]domain.TrackId, 0, len(body.TrackIDs))
	for _, raw := range body.TrackIDs {
		id, err := domain.ParseTrackId(raw)
		if err != nil {
			httputil.HandleServiceError(w, r, domain.ErrInvalidTrackID)
			return
		}
		ids = append(ids, id)
	}

	resolved, err := h.svc.Resolve(r.Context(), userId, ids)
	if err != nil {
		httputil.HandleServiceError(w, r, err)
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
		"duration_ms", time.Since(start).Milliseconds(),
	)
	// Each URL is a bearer link to one user's audio, live for up to
	// ports.MaxPresignTTL, so no cache anywhere may keep this body. These routes
	// are outside the admin tree's security-header middleware (a global one is
	// its own change), hence the per-response headers.
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	httputil.WriteJSON(w, http.StatusOK, resolveAudioURLsResponse{URLs: urls, PrefetchEnabled: h.prefetchEnabled})
}
