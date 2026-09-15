package app

import (
	"altune/go-api/internal/shared/reqmetrics"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

// TestLatencyMiddleware_RecordPanicDoesNotFailRequest is the Plant invariant: a
// panic in the recording path is contained and never reaches the client.
func TestLatencyMiddleware_RecordPanicDoesNotFailRequest(t *testing.T) {
	panicRec := func(string, time.Duration) { panic("boom") }
	r := chi.NewRouter()
	r.Use(latencyMiddleware(panicRec))
	r.Get("/v1/tracks/{trackId}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("ok"))
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/tracks/42", nil))

	if rec.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want %d — a recording panic must not fail the request", rec.Code, http.StatusTeapot)
	}
	if rec.Body.String() != "ok" {
		t.Fatalf("body = %q, want %q", rec.Body.String(), "ok")
	}
}

// TestLatencyMiddleware_RecordsRoutePattern proves the middleware keys latency by
// the chi template (bounded), not the raw path.
func TestLatencyMiddleware_RecordsRoutePattern(t *testing.T) {
	var gotRoute atomic.Value
	rec := func(route string, _ time.Duration) { gotRoute.Store(route) }
	r := chi.NewRouter()
	r.Use(latencyMiddleware(rec))
	r.Get("/v1/tracks/{trackId}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/tracks/42", nil))

	if got := gotRoute.Load(); got != "/v1/tracks/{trackId}" {
		t.Fatalf("recorded route = %v, want /v1/tracks/{trackId}", got)
	}
}

func BenchmarkRecordLatency(b *testing.B) {
	reqmetrics.Observe("/v1/tracks/{trackId}", time.Millisecond) // register in the global registry
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		recordLatency(reqmetrics.Observe, "/v1/tracks/{trackId}", 2*time.Millisecond)
	}
}

// TestRecordLatency_ZeroAlloc is the Plant invariant: the timer's recording path,
// recover guard included, adds no per-request heap allocation.
func TestRecordLatency_ZeroAlloc(t *testing.T) {
	res := testing.Benchmark(BenchmarkRecordLatency)
	if got := res.AllocsPerOp(); got != 0 {
		t.Fatalf("recordLatency = %d allocs/op, want 0", got)
	}
}
