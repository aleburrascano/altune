package httputil

import (
	"altune/go-api/internal/shared/logging"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/google/uuid"
)

const (
	correlationHeader   = "X-Correlation-ID"
	maxCorrelationIDLen = 64
)

func CorrelationID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := inboundCorrelationID(r)
		if id == "" {
			id = uuid.New().String()[:8]
		}
		ctx := logging.WithCorrelationID(r.Context(), id)
		w.Header().Set(correlationHeader, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func inboundCorrelationID(r *http.Request) string {
	id := r.Header.Get(correlationHeader)
	if id == "" || len(id) > maxCorrelationIDLen || !isWellFormedCorrelationID(id) {
		return ""
	}
	return id
}

func isWellFormedCorrelationID(id string) bool {
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func GetCorrelationID(ctx context.Context) string {
	return logging.CorrelationIDFromContext(ctx)
}

func WithCorrelationID(ctx context.Context, id string) context.Context {
	return logging.WithCorrelationID(ctx, id)
}

func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: 200}

		slog.InfoContext(r.Context(), "request.start",
			"method", r.Method,
			"path", r.URL.Path,
			"remote", r.RemoteAddr,
		)

		defer func() {
			duration := time.Since(start)
			level := slog.LevelInfo
			if sw.status >= 500 {
				level = slog.LevelError
			} else if sw.status >= 400 {
				level = slog.LevelWarn
			}

			slog.Log(r.Context(), level, "request.complete",
				"method", r.Method,
				"path", r.URL.Path,
				"status", sw.status,
				"duration", duration,
				"bytes", sw.bytes,
			)
		}()

		next.ServeHTTP(sw, r)
	})
}

func Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.ErrorContext(r.Context(), "panic.recovered",
					"error", fmt.Sprint(rec),
					"method", r.Method,
					"path", r.URL.Path,
					"stack", string(debug.Stack()),
				)
				InternalError(w)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func MaxBodySize(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil && r.ContentLength != 0 {
				r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			}
			next.ServeHTTP(w, r)
		})
	}
}

type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}

func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

var _ http.Flusher = (*statusWriter)(nil)
