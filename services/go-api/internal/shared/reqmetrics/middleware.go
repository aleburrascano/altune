package reqmetrics

import (
	"altune/go-api/internal/shared/runloop"
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func Middleware(record func(route string, d time.Duration, status int)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			recordLatency(r.Context(), record, routePattern(r), time.Since(start), ww.Status())
		})
	}
}

func recordLatency(ctx context.Context, record func(string, time.Duration, int), route string, d time.Duration, status int) {
	defer runloop.RecoverContext(ctx, "reqmetrics.record_panic")
	record(route, d, status)
}

func routePattern(r *http.Request) string {
	if rctx := chi.RouteContext(r.Context()); rctx != nil {
		return rctx.RoutePattern()
	}
	return ""
}
