package handler

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/observe/eventtap"
	"altune/go-api/internal/shared/httputil"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"
)

var (
	streamWriteIdle   = 10 * time.Second
	streamHeartbeat   = 25 * time.Second
	streamMaxLifetime = 15 * time.Minute
)

const keepaliveFrame = ": keepalive\n\n"

var (
	errEventFeedUnavailable = &codedError{
		msg:    "event feed unavailable",
		status: http.StatusServiceUnavailable,
		code:   "observe.event_feed_unavailable",
	}
	errStreamSubscriberLimit = &codedError{
		msg:    "too many observe streams open",
		status: http.StatusTooManyRequests,
		code:   "observe.stream_subscriber_limit",
	}
	errStreamingUnsupported = &codedError{
		msg:    "streaming unsupported",
		status: http.StatusInternalServerError,
		code:   "observe.streaming_unsupported",
	}
)

func streamUnavailable(stream string) *codedError {
	return &codedError{
		msg:    stream + " stream unavailable",
		status: http.StatusServiceUnavailable,
		code:   "observe.stream_unavailable",
	}
}

func rejectSubscription(w http.ResponseWriter, r *http.Request, stream string, err, limitErr error) {
	if !errors.Is(err, limitErr) {
		httputil.HandleServiceError(w, r, streamUnavailable(stream))
		return
	}
	slog.WarnContext(r.Context(), "observe.stream_subscriber_limit", slog.String("stream", stream))
	httputil.HandleServiceError(w, r, errStreamSubscriberLimit)
}

func streamSSE[T any](w http.ResponseWriter, r *http.Request, ch <-chan T) {
	if _, canFlush := w.(http.Flusher); !canFlush {
		httputil.HandleServiceError(w, r, errStreamingUnsupported)
		return
	}
	w = httputil.ExtendWriteDeadlineOnWrite(w, streamWriteIdle)
	rc := http.NewResponseController(w)
	setStreamHeaders(w)
	w.WriteHeader(http.StatusOK)
	if err := writeFrame(w, rc, keepaliveFrame); err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), streamMaxLifetime)
	defer cancel()
	ctx, untilExpiry := auth.UntilTokenExpiry(ctx)
	defer untilExpiry()
	streamFrames(r.WithContext(ctx), w, rc, ch)
}

func (h *Handler) untilShutdown(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	go func() {
		select {
		case <-h.deps.Shutdown:
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}

func setStreamHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Content-Type-Options", "nosniff")
}

func streamFrames[T any](r *http.Request, w http.ResponseWriter, rc *http.ResponseController, ch <-chan T) {
	heartbeat := time.NewTicker(streamHeartbeat)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			if err := writeFrame(w, rc, keepaliveFrame); err != nil {
				return
			}
		case v, open := <-ch:
			if !open {
				return
			}
			if err := forwardValue(w, rc, v); err != nil {
				return
			}
		}
	}
}

func forwardValue[T any](w http.ResponseWriter, rc *http.ResponseController, v T) error {
	frame, sendable := dataFrame(v)
	if !sendable {
		return nil
	}
	return writeFrame(w, rc, frame)
}

func dataFrame[T any](v T) (string, bool) {
	var out any = v
	if ev, isTapEvent := out.(eventtap.TapEvent); isTapEvent {
		out = projectTapEvent(ev)
	}
	payload, err := json.Marshal(out)
	if err != nil {
		return "", false
	}
	return "data: " + string(payload) + "\n\n", true
}

func writeFrame(w http.ResponseWriter, rc *http.ResponseController, frame string) error {
	if _, err := io.WriteString(w, frame); err != nil {
		return err
	}
	return rc.Flush()
}
