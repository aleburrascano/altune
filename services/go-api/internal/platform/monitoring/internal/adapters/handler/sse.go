package handler

import (
	"altune/go-api/internal/platform/monitoring/internal/shell/eventwatch"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/httpserver"
	"altune/go-api/internal/shared/requestuser"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

type StreamBounds struct {
	WriteIdle   time.Duration
	Heartbeat   time.Duration
	MaxLifetime time.Duration
}

const (
	streamWriteIdle   = 10 * time.Second
	streamHeartbeat   = 25 * time.Second
	streamMaxLifetime = 15 * time.Minute
)

func DefaultStreamBounds() StreamBounds {
	return StreamBounds{WriteIdle: streamWriteIdle, Heartbeat: streamHeartbeat, MaxLifetime: streamMaxLifetime}
}

const keepaliveFrame = ": keepalive\n\n"

func rejectSubscription(w http.ResponseWriter, r *http.Request, stream string, err, limitErr error) {
	if !errors.Is(err, limitErr) {
		httpserver.HandleServiceError(w, r, streamUnavailable(stream))
		return
	}
	slog.WarnContext(r.Context(), "observe.stream_subscriber_limit", slog.String("stream", stream))
	httpserver.HandleServiceError(w, r, errStreamSubscriberLimit)
}

func streamSSE[T any](now clock.Func, newTicker clock.TickerFunc, w http.ResponseWriter, r *http.Request, ch <-chan T, bounds StreamBounds) {
	if _, canFlush := w.(http.Flusher); !canFlush {
		httpserver.HandleServiceError(w, r, errStreamingUnsupported)
		return
	}
	w = httpserver.ExtendWriteDeadlineOnWrite(now, w, bounds.WriteIdle)
	rc := http.NewResponseController(w)
	setStreamHeaders(w)
	w.WriteHeader(http.StatusOK)
	if err := writeFrame(w, rc, keepaliveFrame); err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), bounds.MaxLifetime)
	defer cancel()
	ctx, untilExpiry := requestuser.UntilTokenExpiry(ctx)
	defer untilExpiry()
	streamFrames(newTicker, r.WithContext(ctx), w, rc, ch, bounds.Heartbeat)
}

func (h *Handler) untilShutdown(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	background.Go(ctx, "monitoring.sse_shutdown_panic", func() {
		select {
		case <-h.deps.Shutdown:
			cancel()
		case <-ctx.Done():
		}
	})
	return ctx, cancel
}

func setStreamHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Content-Type-Options", "nosniff")
}

func streamFrames[T any](newTicker clock.TickerFunc, r *http.Request, w http.ResponseWriter, rc *http.ResponseController, ch <-chan T, every time.Duration) {
	beats, stop := newTicker(every)
	defer stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-beats:
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
	if ev, isTapEvent := out.(eventwatch.TapEvent); isTapEvent {
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
		return fmt.Errorf("write an sse frame: %w", err)
	}
	if err := rc.Flush(); err != nil {
		return fmt.Errorf("flush an sse frame: %w", err)
	}
	return nil
}
