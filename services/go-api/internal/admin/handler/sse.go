package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"altune/go-api/internal/shared/httputil"
)

// rejectSubscription answers a failed stream Subscribe before any SSE header is
// written: a full subscriber set is a 429 the client can back off from, any
// other failure is a 500.
func rejectSubscription(w http.ResponseWriter, r *http.Request, stream string, err, limitErr error) {
	if !errors.Is(err, limitErr) {
		httputil.InternalError(w, stream+" stream unavailable")
		return
	}
	slog.WarnContext(r.Context(), "admin.stream_subscriber_limit", "stream", stream)
	httputil.HandleServiceError(w, r, errStreamSubscriberLimit)
}

func streamSSE[T any](w http.ResponseWriter, r *http.Request, ch <-chan T) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		httputil.InternalError(w, "streaming unsupported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case v, ok := <-ch:
			if !ok {
				return
			}
			payload, err := json.Marshal(v)
			if err != nil {
				continue
			}
			_, _ = w.Write([]byte("data: "))
			_, _ = w.Write(payload)
			_, _ = w.Write([]byte("\n\n"))
			flusher.Flush()
		}
	}
}
