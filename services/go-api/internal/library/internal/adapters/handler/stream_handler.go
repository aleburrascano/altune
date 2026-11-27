package handler

import (
	audioservice "altune/go-api/internal/library/internal/core/audio"
	"altune/go-api/internal/library/internal/core/ports"
	"altune/go-api/internal/library/internal/domain"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/httpserver"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/requestuser"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

const audioWriteIdleTimeout = 30 * time.Second

type StreamHandler struct {
	svc              *audioservice.StreamTrackService
	writeIdleTimeout time.Duration
	now              clock.Func
	limiter          rateLimitMiddleware
}

var ErrClockRequired = errors.New("handler: the handler requires a clock")

func NewStreamHandler(svc *audioservice.StreamTrackService, now clock.Func, opts ...func(*StreamHandler)) (*StreamHandler, error) {
	if now == nil {
		return nil, fmt.Errorf("NewStreamHandler: %w", ErrClockRequired)
	}
	h := &StreamHandler{svc: svc, writeIdleTimeout: audioWriteIdleTimeout, now: now}
	for _, opt := range opts {
		opt(h)
	}
	h.limiter = newAudioRateLimiter(streamRateLimit, h.now)
	return h, nil
}

func MustNewStreamHandler(svc *audioservice.StreamTrackService, now clock.Func, opts ...func(*StreamHandler)) *StreamHandler {
	h, err := NewStreamHandler(svc, now, opts...)
	if err != nil {
		panic(err)
	}
	return h
}

func WithStreamWriteIdleTimeout(idle time.Duration) func(*StreamHandler) {
	return func(h *StreamHandler) {
		if idle > 0 {
			h.writeIdleTimeout = idle
		}
	}
}

func (h *StreamHandler) Routes(r chi.Router) {
	r.With(h.limiter).Get("/tracks/{trackId}/audio", h.HandleStreamAudio)
	r.With(h.limiter).Post("/tracks/{trackId}/audio/recover", h.HandleRecover)
}

func (h *StreamHandler) HandleStreamAudio(w http.ResponseWriter, r *http.Request) {
	start := h.now()
	w = httpserver.ExtendWriteDeadlineOnWrite(h.now, w, h.writeIdleTimeout)
	userId := requestuser.UserID(r)

	trackId, ok := httpserver.PathID(w, r, "trackId", domain.ParseTrackId, "invalid track ID")
	if !ok {
		return
	}

	out, err := h.svc.Execute(r.Context(), userId, trackId)
	if err != nil {
		httpserver.HandleServiceError(w, r, err)
		return
	}
	defer func() { _ = out.Reader.Close() }()

	slog.InfoContext(r.Context(), "stream.serving",
		logging.KeyTrackID, trackId.String(),
		"size_bytes", out.Size,
		"setup_ms", h.now().Sub(start).Milliseconds(),
	)

	setAudioBodyHeaders(w, *out.Track.AudioRef)
	body := &countedAudioBody{stream: out.Reader}
	http.ServeContent(w, r, "", time.Time{}, body)

	logStreamOutcome(r.Context(), trackId, body, h.now().Sub(start))
}

func setAudioBodyHeaders(w http.ResponseWriter, audioRef string) {
	w.Header().Set("Content-Type", ports.AudioContentType(audioRef))
	w.Header().Set("Cache-Control", "private")
	w.Header().Set("X-Content-Type-Options", "nosniff")
}

func logStreamOutcome(ctx context.Context, trackId domain.TrackId, body *countedAudioBody, elapsed time.Duration) {
	outcome := streamOutcome(ctx, body.readErr)
	attrs := []any{
		logging.KeyTrackID, trackId.String(),
		"outcome", outcome,
		logging.KeyBytes, body.read,
		"total_ms", elapsed.Milliseconds(),
	}
	if outcome != streamOutcomeError {
		slog.InfoContext(ctx, "stream.served", attrs...)
		return
	}
	slog.WarnContext(ctx, "stream.served", append(attrs, logging.KeyError, body.readErr)...)
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
	failed  bool
}

func (b *countedAudioBody) Read(p []byte) (int, error) {
	n, err := b.stream.Read(p)
	b.read += int64(n)
	if err != nil && !errors.Is(err, io.EOF) && !b.failed {
		b.failed = true
		b.readErr = err
	}
	return n, err
}

func (b *countedAudioBody) Seek(offset int64, whence int) (int64, error) {
	return b.stream.Seek(offset, whence)
}

func (h *StreamHandler) HandleRecover(w http.ResponseWriter, r *http.Request) {
	userId := requestuser.UserID(r)

	trackId, ok := httpserver.PathID(w, r, "trackId", domain.ParseTrackId, "invalid track ID")
	if !ok {
		return
	}

	if err := h.svc.RecoverIfMissing(r.Context(), userId, trackId); err != nil {
		httpserver.HandleServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}
