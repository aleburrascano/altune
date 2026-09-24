package reliability

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/goapi"
	"context"
	"sync"
	"testing"
	"time"
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
		if p.bucket == "reliability" && p.series == series {
			out = append(out, p.point)
		}
	}
	return out
}

type steppingClock struct {
	at   time.Time
	step time.Duration
}

func (c *steppingClock) now() time.Time {
	current := c.at
	c.at = c.at.Add(c.step)
	return current
}

func bucketWithSeries(checker reachChecker) (*Bucket, *recordingSeries) {
	b := newBucket(&fakeReader{}, checker, defaultPollInterval)
	series := &recordingSeries{}
	b.UseSeries(series)
	return b, series
}

func TestReachableProbeRecordsUpAndItsLatency(t *testing.T) {
	checker := &fakeChecker{}
	checker.set(goapi.Health{Status: "ok"}, nil)
	b, series := bucketWithSeries(checker)
	clock := &steppingClock{at: time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC), step: 42 * time.Millisecond}
	b.poller.now = clock.now

	b.poller.pollOnce(context.Background())

	up := series.named("up")
	if len(up) != 1 || up[0].Value != 1 {
		t.Fatalf("up series = %+v, want one point of 1", up)
	}
	latency := series.named("latency_ms")
	if len(latency) != 1 || latency[0].Value != 42 {
		t.Fatalf("latency_ms series = %+v, want one point of 42", latency)
	}
	if !latency[0].At.Equal(up[0].At) {
		t.Fatalf("latency at %v and up at %v differ; one probe is one instant", latency[0].At, up[0].At)
	}
}

func TestUnreachableProbeRecordsDownWithoutALatency(t *testing.T) {
	checker := &fakeChecker{}
	checker.set(goapi.Health{}, srcDown("GET /health"))
	b, series := bucketWithSeries(checker)

	b.poller.pollOnce(context.Background())

	if up := series.named("up"); len(up) != 1 || up[0].Value != 0 {
		t.Fatalf("up series = %+v, want one point of 0", up)
	}
	if latency := series.named("latency_ms"); len(latency) != 0 {
		t.Fatalf("latency_ms series = %+v, want none: a probe with no answer has no latency", latency)
	}
}

func TestAnsweredButUnhealthyProbeRecordsDownAndItsLatency(t *testing.T) {
	checker := &fakeChecker{}
	checker.set(goapi.Health{Status: "degraded"}, nil)
	b, series := bucketWithSeries(checker)

	b.poller.pollOnce(context.Background())

	if up := series.named("up"); len(up) != 1 || up[0].Value != 0 {
		t.Fatalf("up series = %+v, want one point of 0", up)
	}
	if latency := series.named("latency_ms"); len(latency) != 1 {
		t.Fatalf("latency_ms series = %+v, want one point: go-api answered", latency)
	}
}

func TestBucketIsASeriesWriter(t *testing.T) {
	var bucket core.Bucket = newBucket(&fakeReader{}, &fakeChecker{}, defaultPollInterval)
	if _, ok := bucket.(core.SeriesWriter); !ok {
		t.Fatal("reliability does not implement core.SeriesWriter, so the composition root never hands it the history store")
	}
}

func TestProbeWithoutSeriesStillPolls(t *testing.T) {
	checker := &fakeChecker{}
	checker.set(goapi.Health{Status: "ok"}, nil)
	b := newBucket(&fakeReader{}, checker, defaultPollInterval)

	b.poller.pollOnce(context.Background())

	if got := b.poller.currentStatus(); got != goapi.StatusUp {
		t.Fatalf("status = %v, want up with no history store wired", got)
	}
}
