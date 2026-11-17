package usage_test

import (
	"altune/overseer/internal/buckets/usage"
	"altune/overseer/internal/core"
	"altune/overseer/internal/goapi"
	"context"
	"sync"
	"testing"
	"time"
)

const (
	requestsPerMin = "requests_per_min"
	activeUsers    = "active_users"
)

type recordedPoint struct {
	bucket string
	series string
	point  core.Point
}

type recordingSeries struct {
	mu     sync.Mutex
	points []recordedPoint
}

func (r *recordingSeries) Record(bucket, series string, p core.Point) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.points = append(r.points, recordedPoint{bucket: bucket, series: series, point: p})
}

func (r *recordingSeries) Query(string, string, time.Time, time.Time) ([]core.Point, error) {
	return nil, nil
}

func (r *recordingSeries) named(series string) []core.Point {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []core.Point
	for _, p := range r.points {
		if p.bucket == "usage" && p.series == series {
			out = append(out, p.point)
		}
	}
	return out
}

func bucketWithSeries(src eventSource) (*usage.Bucket, *recordingSeries) {
	b := newBucket(src)
	series := &recordingSeries{}
	b.UseSeries(series)
	return b, series
}

func TestCollectRecordsRequestsAndUsersOncePerCompletedWindow(t *testing.T) {
	src := newFakeSource(16)
	b, series := bucketWithSeries(src)

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	src.push(goapi.Event{Type: "search_performed", Timestamp: base, User: "alice", Subject: "jazz"})
	src.push(goapi.Event{Type: "play", Timestamp: base.Add(10 * time.Second), User: "bob"})
	src.push(goapi.Event{Type: "play", Timestamp: base.Add(20 * time.Second), User: "alice"})
	collectStore(t, b)

	if got := series.named(requestsPerMin); len(got) != 0 {
		t.Fatalf("requests_per_min recorded %d points before the window completed, want 0: %+v", len(got), got)
	}

	src.push(goapi.Event{Type: "play", Timestamp: base.Add(90 * time.Second), User: "carol"})
	collectStore(t, b)

	reqPoints := series.named(requestsPerMin)
	if len(reqPoints) != 1 {
		t.Fatalf("requests_per_min recorded %d points, want 1: %+v", len(reqPoints), reqPoints)
	}
	if reqPoints[0].Value != 3 {
		t.Errorf("requests_per_min = %v, want 3", reqPoints[0].Value)
	}
	userPoints := series.named(activeUsers)
	if len(userPoints) != 1 {
		t.Fatalf("active_users recorded %d points, want 1: %+v", len(userPoints), userPoints)
	}
	if userPoints[0].Value != 2 {
		t.Errorf("active_users = %v, want 2 (alice, bob)", userPoints[0].Value)
	}
}

func TestKeySeriesIsRequestsPerMin(t *testing.T) {
	b := newBucket(newFakeSource(1))
	if got := b.KeySeries(); got != requestsPerMin {
		t.Errorf("KeySeries() = %q, want %q", got, requestsPerMin)
	}
}

func TestBucketIsASeriesWriter(t *testing.T) {
	var bucket core.Bucket = newBucket(newFakeSource(1))
	if _, ok := bucket.(core.SeriesWriter); !ok {
		t.Fatal("usage does not implement core.SeriesWriter, so the composition root never hands it the history store")
	}
}

func TestCollectWithoutSeriesStillCollects(t *testing.T) {
	src := newFakeSource(1)
	b := newBucket(src)
	src.push(goapi.Event{Type: "play", Subject: "song"})

	if _, err := b.Collect(context.Background()); err != nil {
		t.Fatalf("Collect with no series wired: %v", err)
	}
}
