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

func WithStreamRateLimit(limit AudioRateLimit) func(*StreamHandler) {
	return func(h *StreamHandler) { h.rateLimit = limit }
}

func withStreamClock(now func() time.Time) func(*StreamHandler) {
	return func(h *StreamHandler) { h.now = now }
}

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

func setAudioBodyHeaders(w http.ResponseWriter, audioRef string) {
	w.Header().Set("Content-Type", ports.AudioContentType(audioRef))
	w.Header().Set("Cache-Control", "private")
	w.Header().Set("X-Content-Type-Options", "nosniff")
}

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
