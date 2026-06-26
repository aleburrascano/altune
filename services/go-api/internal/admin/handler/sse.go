package handler

import (
	"encoding/json"
	"net/http"

	"altune/go-api/internal/shared/httputil"
)

// streamSSE encodes each value received on ch as a Server-Sent Event until the
// request context is cancelled (client disconnect) or the channel closes. It
// requires an http.Flusher; the chi RequestLogger wrapper forwards Flush, so
// the operator console's streams work behind the middleware chain.
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
