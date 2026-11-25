package reqmetrics_test

import (
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/reqmetrics"
	"context"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestLatencyMiddleware_RecordPanicIsLoggedOnceWithTheRequestID(t *testing.T) {
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	ring := logging.Setup("error", false)
	panicRec := func(string, time.Duration, int) { panic("histogram exploded") }
	req := httptest.NewRequest(http.MethodGet, "/v1/tracks/42", nil)
	req = req.WithContext(logging.WithCorrelationID(context.Background(), "corr-reqm-3"))

	routerRecordingWith(panicRec, http.StatusOK).ServeHTTP(httptest.NewRecorder(), req)

	var found []logging.CapturedRecord
	for _, r := range ring.Snapshot() {
		if r.Message == "reqmetrics.record_panic" {
			found = append(found, r)
		}
	}
	if len(found) != 1 {
		t.Fatalf("captured %d reqmetrics.record_panic records, want exactly 1: a metrics panic must not be silent", len(found))
	}
	if found[0].Level != slog.LevelError.String() || found[0].Attrs["corr_id"] != "corr-reqm-3" {
		t.Errorf("record = level %s corr_id %q, want ERROR with the request's corr_id", found[0].Level, found[0].Attrs["corr_id"])
	}
	if !strings.Contains(found[0].Attrs["panic"], "histogram exploded") {
		t.Errorf("panic = %q, want the recovered value", found[0].Attrs["panic"])
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

const allocTrials = 40

func steadyStateAllocs(f func()) float64 {
	fewest := math.MaxFloat64
	for range allocTrials {
		fewest = min(fewest, testing.AllocsPerRun(1, f))
	}
	return fewest
}

func TestLatencyMiddleware_RecordingIntoTheRegistryAllocatesNothingBeyondWrappingTheWriter(t *testing.T) {
	recording := routerWith(reqmetrics.Middleware(reqmetrics.NewRegistry().Observe), http.StatusOK)
	wrappingOnly := routerWith(wrapResponseWriterOnly, http.StatusOK)
	serveTrack(recording)

	withRegistry := steadyStateAllocs(func() { serveTrack(recording) })
	withWrapOnly := steadyStateAllocs(func() { serveTrack(wrappingOnly) })

	if withRegistry != withWrapOnly {
		t.Fatalf("a request through the middleware recording into a registry = %v allocs, one that only wraps the writer = %v, want the same", withRegistry, withWrapOnly)
	}
}
