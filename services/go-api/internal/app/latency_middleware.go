package app

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func latencyMiddleware(record func(route string, d time.Duration, status int)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			recordLatency(record, routePattern(r), time.Since(start), ww.Status())
		})
	}
}

func recordLatency(record func(string, time.Duration, int), route string, d time.Duration, status int) {
	defer func() { _ = recover() }()
	record(route, d, status)
}

func routePattern(r *http.Request) string {
	if rctx := chi.RouteContext(r.Context()); rctx != nil {
		return rctx.RoutePattern()
	}
	return ""
}
