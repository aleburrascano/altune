package shell_test

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/shell"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type keyedStubBucket struct {
	stubBucket
	key string
}

func (s keyedStubBucket) KeySeries() string { return s.key }

type erroringSeries struct{}

func (erroringSeries) Names(string) ([]string, error) { return nil, nil }

func (erroringSeries) Query(string, string, time.Time, time.Time) ([]core.Point, error) {
	return nil, context.DeadlineExceeded
}

type bucketsBody struct {
	Buckets []struct {
		ID    string `json:"id"`
		Spark []struct {
			At string  `json:"at"`
			V  float64 `json:"v"`
		} `json:"spark"`
	} `json:"buckets"`
}

func getBuckets(t *testing.T, srv http.Handler) (*httptest.ResponseRecorder, bucketsBody) {
	t.Helper()
	rec := do(srv, withOwner(httptest.NewRequest(http.MethodGet, "/api/buckets", nil)))
	var body bucketsBody
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode /api/buckets: %v (%s)", err, rec.Body.String())
		}
	}
	return rec, body
}

func TestBucketsSparkHoldsAtMostThirtyPointsFromTheTail(t *testing.T) {
	at := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	points := make([]core.Point, 0, 40)
	for i := 0; i < 40; i++ {
		points = append(points, core.Point{At: at.Add(time.Duration(i) * time.Second), Value: float64(i)})
	}
	source := &fakeSeries{points: map[string][]core.Point{"latency_ms": points}}
	reg := fixedRegistry{buckets: []core.Bucket{
		keyedStubBucket{stubBucket: stubBucket{id: "reliability", state: core.StateLive}, key: "latency_ms"},
	}}
	srv := newServer(reg, shell.WithSeries(source))

	rec, body := getBuckets(t, srv)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if len(body.Buckets) != 1 {
		t.Fatalf("buckets = %d, want 1", len(body.Buckets))
	}
	spark := body.Buckets[0].Spark
	if len(spark) != 30 {
		t.Fatalf("spark holds %d points, want 30", len(spark))
	}
	if spark[0].V != 10 {
		t.Fatalf("spark[0].V = %v, want 10 (the tail of the last 30 of 40)", spark[0].V)
	}
	if spark[29].V != 39 {
		t.Fatalf("spark[29].V = %v, want 39 (the most recent point)", spark[29].V)
	}
}

func TestBucketsOmitSparkWhenTheBucketHasNoKeySeries(t *testing.T) {
	reg := fixedRegistry{buckets: []core.Bucket{stubBucket{id: "cost", state: core.StateLive}}}
	source := &fakeSeries{points: map[string][]core.Point{"anything": {{At: time.Now(), Value: 1}}}}
	srv := newServer(reg, shell.WithSeries(source))

	rec, _ := getBuckets(t, srv)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); jsonHasKey(got, "spark") {
		t.Fatalf("body contains a spark field for a bucket with no KeySeries: %s", got)
	}
}

func jsonHasKey(body, key string) bool {
	var envelope struct {
		Buckets []map[string]json.RawMessage `json:"buckets"`
	}
	if err := json.Unmarshal([]byte(body), &envelope); err != nil {
		return false
	}
	for _, bucket := range envelope.Buckets {
		if _, ok := bucket[key]; ok {
			return true
		}
	}
	return false
}

func TestBucketsOmitSparkWhenTheHistoryReadFails(t *testing.T) {
	reg := fixedRegistry{buckets: []core.Bucket{
		keyedStubBucket{stubBucket: stubBucket{id: "reliability", state: core.StateLive}, key: "latency_ms"},
	}}
	srv := newServer(reg, shell.WithSeries(erroringSeries{}))

	rec, body := getBuckets(t, srv)

	if rec.Code != http.StatusOK {
		t.Fatalf("a history read failure must degrade the spark, not the whole response; status = %d (%s)", rec.Code, rec.Body.String())
	}
	if len(body.Buckets) != 1 || body.Buckets[0].Spark != nil {
		t.Fatalf("spark = %+v, want none when the history read fails", body.Buckets)
	}
}

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
