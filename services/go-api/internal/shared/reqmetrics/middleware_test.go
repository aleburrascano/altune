package reqmetrics_test

import (
	"altune/go-api/internal/shared/reqmetrics"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func routerRecordingWith(record func(string, time.Duration, int), status int) http.Handler {
	return routerWith(reqmetrics.Middleware(record), status)
}

func routerWith(mw func(http.Handler) http.Handler, status int) http.Handler {
	r := chi.NewRouter()
	r.Use(mw)
	r.Get("/v1/tracks/{trackId}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte("ok"))
	})
	return r
}

func serveTrack(h http.Handler) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/tracks/42", nil))
	return rec
}

func TestLatencyMiddleware_RecordPanicDoesNotFailRequest(t *testing.T) {
	panicRec := func(string, time.Duration, int) { panic("boom") }

	rec := serveTrack(routerRecordingWith(panicRec, http.StatusTeapot))

	if rec.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want %d, a recording panic must not fail the request", rec.Code, http.StatusTeapot)
	}
	if rec.Body.String() != "ok" {
		t.Fatalf("body = %q, want %q", rec.Body.String(), "ok")
	}
}

func TestLatencyMiddleware_RecordsRoutePattern(t *testing.T) {
	var gotRoute atomic.Value
	rec := func(route string, _ time.Duration, _ int) { gotRoute.Store(route) }

	serveTrack(routerRecordingWith(rec, http.StatusOK))

	if got := gotRoute.Load(); got != "/v1/tracks/{trackId}" {
		t.Fatalf("recorded route = %v, want /v1/tracks/{trackId}", got)
	}
}

func TestLatencyMiddleware_RecordsAnEmptyRouteOutsideARouter(t *testing.T) {
	gotRoute := "unset"
	rec := func(route string, _ time.Duration, _ int) { gotRoute = route }
	h := reqmetrics.Middleware(rec)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	serveTrack(h)

	if gotRoute != "" {
		t.Fatalf("recorded route = %q, want empty when no router matched", gotRoute)
	}
}

func TestLatencyMiddleware_RecordsResponseStatus(t *testing.T) {
	var gotStatus atomic.Int64
	rec := func(_ string, _ time.Duration, status int) { gotStatus.Store(int64(status)) }

	serveTrack(routerRecordingWith(rec, http.StatusServiceUnavailable))

	if got := gotStatus.Load(); got != http.StatusServiceUnavailable {
		t.Fatalf("recorded status = %d, want %d", got, http.StatusServiceUnavailable)
	}
}

func wrapResponseWriterOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(middleware.NewWrapResponseWriter(w, r.ProtoMajor), r)
	})
}

func TestLatencyMiddleware_RecordingIntoTheRegistryAllocatesNothingBeyondWrappingTheWriter(t *testing.T) {
	recording := routerWith(reqmetrics.Middleware(reqmetrics.Observe), http.StatusOK)
	wrappingOnly := routerWith(wrapResponseWriterOnly, http.StatusOK)
	serveTrack(recording)

	withRegistry := testing.AllocsPerRun(200, func() { serveTrack(recording) })
	withWrapOnly := testing.AllocsPerRun(200, func() { serveTrack(wrappingOnly) })

	if withRegistry != withWrapOnly {
		t.Fatalf("a request through the middleware recording into Observe = %v allocs, one that only wraps the writer = %v, want the same", withRegistry, withWrapOnly)
	}
}
