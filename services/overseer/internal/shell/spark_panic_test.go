package shell_test

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/shell"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// panicKeySeriesBucket panics inside KeySeries, the seam h.spark reads before it
// ever reaches the store.
type panicKeySeriesBucket struct{ stubBucket }

func (panicKeySeriesBucket) KeySeries() string { panic("keyseries blew up") }

// panicSeries panics inside the history read h.spark drives.
type panicSeries struct{}

func (panicSeries) Names(string) ([]string, error) { return nil, nil }

func (panicSeries) Query(string, string, time.Time, time.Time) ([]core.Point, error) {
	panic("query blew up")
}

// TestBucketsSurviveAPanickingKeySeries proves a panic in a bucket's KeySeries
// degrades only that bucket's spark to nil rather than crashing the whole
// /api/buckets response — the same degrade-don't-crash guarantee safeSnapshot
// gives Snapshot, now extended to the spark read that runs alongside it.
func TestBucketsSurviveAPanickingKeySeries(t *testing.T) {
	reg := fixedRegistry{buckets: []core.Bucket{
		panicKeySeriesBucket{stubBucket: stubBucket{id: "boom", state: core.StateLive}},
		stubBucket{id: "ok", state: core.StateLive},
	}}
	srv := newServer(reg)

	rec, body := getBuckets(t, srv)

	if rec.Code != http.StatusOK {
		t.Fatalf("a panicking KeySeries must degrade one bucket's spark, not the response; status = %d (%s)", rec.Code, rec.Body.String())
	}
	if len(body.Buckets) != 2 {
		t.Fatalf("buckets = %d, want 2 despite the panic", len(body.Buckets))
	}
}

// TestBucketsSurviveAPanickingHistoryRead proves the same containment when the
// panic happens one level deeper, inside the history read itself.
func TestBucketsSurviveAPanickingHistoryRead(t *testing.T) {
	reg := fixedRegistry{buckets: []core.Bucket{
		keyedStubBucket{stubBucket: stubBucket{id: "reliability", state: core.StateLive}, key: "latency_ms"},
	}}
	srv := newServer(reg, shell.WithSeries(panicSeries{}))

	rec, body := getBuckets(t, srv)

	if rec.Code != http.StatusOK {
		t.Fatalf("a panicking history read must degrade the spark, not the response; status = %d (%s)", rec.Code, rec.Body.String())
	}
	if len(body.Buckets) != 1 || body.Buckets[0].Spark != nil {
		t.Fatalf("spark = %+v, want none when the history read panics", body.Buckets)
	}
}

// TestStreamSurvivesAPanickingKeySeries proves the SSE path, which drives the
// same snapshots() through emitAll, gets the same containment: a panicking
// bucket among several never stops the frame that carries the rest.
func TestStreamSurvivesAPanickingKeySeries(t *testing.T) {
	reg := fixedRegistry{buckets: []core.Bucket{
		panicKeySeriesBucket{stubBucket: stubBucket{id: "boom", state: core.StateLive}},
		stubBucket{id: "ok", state: core.StateLive},
	}}
	srv := newServer(reg, shell.WithStreamInterval(5*time.Millisecond))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	req := withOwner(httptest.NewRequest(http.MethodGet, "/api/stream", nil)).WithContext(ctx)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("stream status = %d, want 200 despite a panicking KeySeries", rec.Code)
	}
	if got := rec.Body.String(); got == "" {
		t.Fatalf("stream body is empty, want the initial frame despite the panic")
	}
}
