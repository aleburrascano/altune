package requestmetrics

import (
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/clock"
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func Middleware(now clock.Func, record func(route string, d time.Duration, status int)) func(http.Handler) http.Handler {
	if now == nil {
		panic("requestmetrics.Middleware: a nil clock")
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			recordLatency(r.Context(), record, routePattern(r), now().Sub(start), ww.Status())
		})
	}
}

func recordLatency(ctx context.Context, record func(string, time.Duration, int), route string, d time.Duration, status int) {
	defer background.RecoverContext(ctx, "reqmetrics.record_panic")
	record(route, d, status)
}

func routePattern(r *http.Request) string {
	if rctx := chi.RouteContext(r.Context()); rctx != nil {
		return rctx.RoutePattern()
	}
	return ""
}
