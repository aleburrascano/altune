package httpserver

import (
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/logging"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
)

func RequestLogger(now clock.Func) func(http.Handler) http.Handler {
	if now == nil {
		panic("httpserver.RequestLogger: a clock is required")
	}
	return func(next http.Handler) http.Handler {
		return requestLogger(next, now)
	}
}

func requestLogger(next http.Handler, now clock.Func) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := now()
		sw := newStatusWriter(w)
		ctx, scope := withRequestScope(r.Context())
		r = r.WithContext(ctx)
		defer func(r *http.Request) {
			attrs := []any{
				logging.KeyMethod, r.Method,
				logging.KeyPath, r.URL.Path,
				logging.KeyRoute, routePattern(r),
				logging.KeyStatus, sw.status,
				logging.KeyDuration, now().Sub(start),
				logging.KeyBytes, sw.bytes,
			}
			if userID := scope.user(); userID != "" {
				attrs = append(attrs, logging.KeyUserID, userID)
			}
			slog.InfoContext(r.Context(), "request.complete", attrs...)
		}(r)
		next.ServeHTTP(sw, r)
	})
}

func routePattern(r *http.Request) string {
	if rc := chi.RouteContext(r.Context()); rc != nil {
		if pattern := rc.RoutePattern(); pattern != "" {
			return pattern
		}
	}
	return unmatchedRoute
}

const unmatchedRoute = "unmatched"

type statusWriter struct {
	http.ResponseWriter
	status           int
	bytes            int
	hasWrittenHeader bool
}

func newStatusWriter(w http.ResponseWriter) *statusWriter {
	return &statusWriter{ResponseWriter: w, status: http.StatusOK}
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.hasWrittenHeader = true
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	w.hasWrittenHeader = true
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}

func (w *statusWriter) Flush() {
	w.hasWrittenHeader = true
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

var _ http.Flusher = (*statusWriter)(nil)
