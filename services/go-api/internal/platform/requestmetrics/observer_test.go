package requestmetrics_test

import (
	"altune/go-api/internal/platform/requestmetrics"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/httpserver"
	"altune/go-api/internal/shared/logging"
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
)

func routerRecordingWith(record func(string, time.Duration, int), status int) http.Handler {
	return routerWith(httpserver.ObserveRequests(clock.Now, requestmetrics.Observer(record)), status)
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

func TestObserver_RecordPanicDoesNotFailRequest(t *testing.T) {
	panicRec := func(string, time.Duration, int) { panic("boom") }

	rec := serveTrack(routerRecordingWith(panicRec, http.StatusTeapot))

	if rec.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want %d, a recording panic must not fail the request", rec.Code, http.StatusTeapot)
	}
	if rec.Body.String() != "ok" {
		t.Fatalf("body = %q, want %q", rec.Body.String(), "ok")
	}
}

func TestObserver_RecordPanicIsLoggedOnceWithTheRequestID(t *testing.T) {
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	ring := logging.Setup("error")
	panicRec := func(string, time.Duration, int) { panic("histogram exploded") }
	req := httptest.NewRequest(http.MethodGet, "/v1/tracks/42", nil)
	req = req.WithContext(logging.ContextWithCorrelationID(context.Background(), "corr-reqm-3"))

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

func TestObserver_RecordsRoutePattern(t *testing.T) {
	var gotRoute atomic.Value
	rec := func(route string, _ time.Duration, _ int) { gotRoute.Store(route) }

	serveTrack(routerRecordingWith(rec, http.StatusOK))

	if got := gotRoute.Load(); got != "/v1/tracks/{trackId}" {
		t.Fatalf("recorded route = %v, want /v1/tracks/{trackId}", got)
	}
}

func TestObserver_RecordsTheUnmatchedRouteOutsideARouter(t *testing.T) {
	gotRoute := "unset"
	rec := func(route string, _ time.Duration, _ int) { gotRoute = route }
	h := httpserver.ObserveRequests(clock.Now, requestmetrics.Observer(rec))(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	serveTrack(h)

	if gotRoute != "unmatched" {
		t.Fatalf("recorded route = %q, want unmatched when no router matched", gotRoute)
	}
}

func TestObserver_RecordsResponseStatus(t *testing.T) {
	var gotStatus atomic.Int64
	rec := func(_ string, _ time.Duration, status int) { gotStatus.Store(int64(status)) }

	serveTrack(routerRecordingWith(rec, http.StatusServiceUnavailable))

	if got := gotStatus.Load(); got != http.StatusServiceUnavailable {
		t.Fatalf("recorded status = %d, want %d", got, http.StatusServiceUnavailable)
	}
}

const allocTrials = 40

func steadyStateAllocs(f func()) float64 {
	fewest := math.MaxFloat64
	for range allocTrials {
		fewest = min(fewest, testing.AllocsPerRun(1, f))
	}
	return fewest
}

func capturingObserver(r *http.Request) (*http.Request, func(httpserver.Outcome)) {
	return r, func(httpserver.Outcome) { _ = r }
}

func TestObserver_RecordingIntoTheRegistryAllocatesNothingBeyondTheMeasurement(t *testing.T) {
	recording := routerWith(httpserver.ObserveRequests(clock.Now, requestmetrics.Observer(requestmetrics.NewRegistry().Observe)), http.StatusOK)
	measuringOnly := routerWith(httpserver.ObserveRequests(clock.Now, capturingObserver), http.StatusOK)
	serveTrack(recording)

	withRegistry := steadyStateAllocs(func() { serveTrack(recording) })
	withoutRegistry := steadyStateAllocs(func() { serveTrack(measuringOnly) })

	if withRegistry != withoutRegistry {
		t.Fatalf("a request recorded into a registry = %v allocs, one that is only measured = %v, want the same", withRegistry, withoutRegistry)
	}
}
