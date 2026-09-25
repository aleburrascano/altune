package handler

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/catalog/service"
	"altune/go-api/internal/shared/httputil"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

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
// Both are throttled per user out of the same bucket: recover costs a DB read
// plus a storage HEAD and answers 202 either way, so unthrottled it is a free
// storage probe. The client fires it once per playback error, far under the
// stream budget it now shares.
func (h *StreamHandler) Routes(r chi.Router) {
	r.With(h.limiter.middleware).Get("/tracks/{trackId}/audio", h.HandleStreamAudio)
	r.With(h.limiter.middleware).Post("/tracks/{trackId}/audio/recover", h.HandleRecover)
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

	setAudioBodyHeaders(w, *out.Track.AudioRef)
	body := &countedAudioBody{stream: out.Reader}
	http.ServeContent(w, r, "", time.Time{}, body)

	logStreamOutcome(r.Context(), trackId, body, time.Since(start))
}

// setAudioBodyHeaders frames one user's private audio. These routes sit outside
// the admin tree's security-header middleware (a global one is its own change),
// so they carry their own: no shared cache may keep the bytes, and a sniffed
// type on a user-supplied upload must not become script. Caching is `private`
// rather than `no-store` so a player's own cache still serves seeks and
// replays, which is exactly the Range traffic no-store would send back here.
func setAudioBodyHeaders(w http.ResponseWriter, audioRef string) {
	w.Header().Set("Content-Type", ports.AudioContentType(audioRef))
	w.Header().Set("Cache-Control", "private")
	w.Header().Set("X-Content-Type-Options", "nosniff")
}

// logStreamOutcome closes the stream.served line over what http.ServeContent
// does not report: how much of the body came out of storage, and whether the
// transfer ended whole. Without it a truncated stream logs as a success.
func logStreamOutcome(ctx context.Context, trackId domain.TrackId, body *countedAudioBody, elapsed time.Duration) {
	outcome := streamOutcome(ctx, body.readErr)
	attrs := []any{
		"track_id", trackId.String(),
		"outcome", outcome,
		"bytes", body.read,
		"total_ms", elapsed.Milliseconds(),
	}
	if outcome != streamOutcomeError {
		slog.InfoContext(ctx, "stream.served", attrs...)
		return
	}
	slog.WarnContext(ctx, "stream.served", append(attrs, "error", body.readErr)...)
}

// How a transfer ended. Only streamOutcomeError is this server's failure and
// worth a warning: a player abandons Range reads on every seek and track skip,
// and those cancellations reach the store as read errors too.
const (
	streamOutcomeOK      = "ok"
	streamOutcomeAborted = "aborted"
	streamOutcomeError   = "error"
)

func streamOutcome(ctx context.Context, readErr error) string {
	switch {
	case ctx.Err() != nil:
		return streamOutcomeAborted
	case readErr != nil:
		return streamOutcomeError
	default:
		return streamOutcomeOK
	}
}

// countedAudioBody records the bytes http.ServeContent read out of the stream
// and the first storage read failure, neither of which ServeContent returns.
// One request reads it from one goroutine, and the count is read only after
// ServeContent returns, so it needs no lock.
type countedAudioBody struct {
	stream  ports.AudioStream
	read    int64
	readErr error
}

func (b *countedAudioBody) Read(p []byte) (int, error) {
	n, err := b.stream.Read(p)
	b.read += int64(n)
	if err != nil && !errors.Is(err, io.EOF) && b.readErr == nil {
		b.readErr = err
	}
	return n, err
}

func (b *countedAudioBody) Seek(offset int64, whence int) (int64, error) {
	return b.stream.Seek(offset, whence)
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
