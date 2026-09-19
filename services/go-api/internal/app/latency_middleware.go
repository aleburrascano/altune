package app

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// latencyMiddleware times each request end-to-end and records its duration and
// response status under the matched chi route pattern. It wraps the writer to
// observe the status the downstream chain sets; the wrapper is transparent
// (Unwrap and Flush pass through), so the write-deadline and SSE paths below it
// are unaffected. Recording is best-effort: it runs after the response is
// served, and a panic in it is recovered, so instrumentation can never fail or
// slow a request. record is injected so tests can force a panic; production
// wires reqmetrics.Observe.
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

// recordLatency runs the injected recorder under a recover so a panic in the
// metrics path is dropped, never propagating to the request. The recover adds
// no per-request heap allocation (a benchmark asserts it).
func recordLatency(record func(string, time.Duration, int), route string, d time.Duration, status int) {
	defer func() { _ = recover() }()
	record(route, d, status)
}

// routePattern is the chi route template matched for r (e.g.
// "/v1/tracks/{trackId}"), or "" when nothing matched. The template — never the
// raw path — keeps the histogram's route cardinality bounded.
func routePattern(r *http.Request) string {
	if rctx := chi.RouteContext(r.Context()); rctx != nil {
		return rctx.RoutePattern()
	}
	return ""
}
