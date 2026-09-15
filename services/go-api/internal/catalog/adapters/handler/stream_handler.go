package handler

import (
	"log/slog"
	"net/http"
	"time"

	"altune/go-api/internal/auth"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/catalog/service"
	"altune/go-api/internal/shared/httputil"

	"github.com/go-chi/chi/v5"
)

// audioWriteIdleTimeout is how long an audio response may go without the client
// accepting another write. A track can take minutes to send over a slow mobile
// network, so it is not bound by the API's total write deadline; only a client
// that stops reading for this long is cut off, and players resume with a Range
// request.
const audioWriteIdleTimeout = 30 * time.Second

type StreamHandler struct {
	svc              *service.StreamTrackService
	writeIdleTimeout time.Duration
	rateLimit        AudioRateLimit
	now              func() time.Time
	limiter          *audioRateLimiter
}

func NewStreamHandler(svc *service.StreamTrackService, opts ...func(*StreamHandler)) *StreamHandler {
	h := &StreamHandler{svc: svc, writeIdleTimeout: audioWriteIdleTimeout, rateLimit: DefaultStreamRateLimit, now: time.Now}
	for _, opt := range opts {
		opt(h)
	}
	h.limiter = newAudioRateLimiter(h.rateLimit, h.now)
	return h
}

// WithStreamRateLimit replaces DefaultStreamRateLimit.
func WithStreamRateLimit(limit AudioRateLimit) func(*StreamHandler) {
	return func(h *StreamHandler) { h.rateLimit = limit }
}

// withStreamClock injects the limiter's clock so tests can refill buckets
// without sleeping.
func withStreamClock(now func() time.Time) func(*StreamHandler) {
	return func(h *StreamHandler) { h.now = now }
}

// Routes registers the stream endpoints on r. These paths interleave with the
// /tracks tree, so the handler registers directly onto the shared router rather
// than returning a mountable chi.Router like LibraryHandler/PlaylistHandler/
// TrackHandler. Keeps the paths byte-identical to their previous hand-wiring.
// The audio GET is throttled per user; recover is not, it only fires on a
// playback error.
func (h *StreamHandler) Routes(r chi.Router) {
	r.With(h.limiter.middleware).Get("/tracks/{trackId}/audio", h.HandleStreamAudio)
	r.Post("/tracks/{trackId}/audio/recover", h.HandleRecover)
}

func (h *StreamHandler) HandleStreamAudio(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	w = httputil.ExtendWriteDeadlineOnWrite(w, h.writeIdleTimeout)
	userId, ok := auth.RequireUserID(w, r)
	if !ok {
		return
	}

	trackId, ok := httputil.PathID(w, r, "trackId", domain.ParseTrackId, "invalid track ID")
	if !ok {
		return
	}

	out, err := h.svc.Execute(r.Context(), userId, trackId)
	if err != nil {
		httputil.HandleServiceError(w, r, err)
		return
	}
	defer out.Reader.Close()

	slog.InfoContext(r.Context(), "stream.serving",
		"track_id", trackId.String(),
		"size_bytes", out.Size,
		"setup_ms", time.Since(start).Milliseconds(),
	)

	w.Header().Set("Content-Type", ports.AudioContentType(*out.Track.AudioRef))
	http.ServeContent(w, r, "", time.Time{}, out.Reader)

	slog.InfoContext(r.Context(), "stream.served",
		"track_id", trackId.String(),
		"total_ms", time.Since(start).Milliseconds(),
	)
}

func (h *StreamHandler) HandleRecover(w http.ResponseWriter, r *http.Request) {
	userId, ok := auth.RequireUserID(w, r)
	if !ok {
		return
	}

	trackId, ok := httputil.PathID(w, r, "trackId", domain.ParseTrackId, "invalid track ID")
	if !ok {
		return
	}

	if err := h.svc.RecoverIfMissing(r.Context(), userId, trackId); err != nil {
		httputil.HandleServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}
