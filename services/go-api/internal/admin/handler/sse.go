package handler

import (
	"altune/go-api/internal/admin/eventtap"
	"altune/go-api/internal/auth"
	"altune/go-api/internal/shared/httputil"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// A live tail replaces the route-level write deadline with a per-write one, so
// it outlives the route budget while a client that stops reading still fails a
// write within streamWriteIdle, rather than pinning the handler goroutine and
// its subscriber slot until the process restarts (#2004). streamHeartbeat
// bounds how long a silent stream can go without a write, which is what makes
// that deadline fire on an otherwise idle connection.
var (
	streamWriteIdle = 10 * time.Second
	streamHeartbeat = 25 * time.Second
)

var streamMaxLifetime = 15 * time.Minute

// keepaliveFrame is an SSE comment: a client ignores it, so it proves the
// connection is still writable without being mistaken for an event.
const keepaliveFrame = ": keepalive\n\n"

// rejectSubscription answers a failed stream Subscribe before any SSE header is
// written: a full subscriber set is a 429 the client can back off from, any
// other failure is a 503 it can retry.
func rejectSubscription(w http.ResponseWriter, r *http.Request, stream string, err, limitErr error) {
	if !errors.Is(err, limitErr) {
		httputil.HandleServiceError(w, r, streamUnavailable(stream))
		return
	}
	slog.WarnContext(r.Context(), "admin.stream_subscriber_limit", "stream", stream)
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
	// The opening keepalive flushes the head under the idle deadline the first
	// Write installs, so even the handshake cannot block unbounded.
	if err := writeFrame(w, rc, keepaliveFrame); err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), streamMaxLifetime)
	defer cancel()
	ctx, untilExpiry := auth.UntilTokenExpiry(ctx)
	defer untilExpiry()
	streamFrames(r.WithContext(ctx), w, rc, ch)
}

func (h *AdminHandler) untilShutdown(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	go func() {
		select {
		case <-h.shutdown:
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
}

// streamFrames forwards ch until the request ends, the source closes, or a
// write fails. A failed write means the client is gone or has stalled past the
// idle deadline, and returning is what releases its subscriber slot.
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
			frame, sendable := dataFrame(v)
			if !sendable {
				continue
			}
			if err := writeFrame(w, rc, frame); err != nil {
				return
			}
		}
	}
}

// dataFrame renders v as one SSE data frame. It is not sendable when v cannot
// be marshalled, which is a fault in that one value and no reason to end the
// stream.
func dataFrame[T any](v T) (string, bool) {
	var out any = v
	if ev, ok := out.(eventtap.TapEvent); ok {
		out = projectTapEvent(ev)
	}
	payload, err := json.Marshal(out)
	if err != nil {
		return "", false
	}
	return "data: " + string(payload) + "\n\n", true
}

// writeFrame emits frame in a single Write, so the idle deadline renewed per
// Write bounds the whole frame instead of each of its parts.
func writeFrame(w http.ResponseWriter, rc *http.ResponseController, frame string) error {
	if _, err := io.WriteString(w, frame); err != nil {
		return err
	}
	return rc.Flush()
}
