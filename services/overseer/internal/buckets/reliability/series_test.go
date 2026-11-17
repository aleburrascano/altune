package reliability_test

import (
	"altune/overseer/internal/buckets/reliability"
	"altune/overseer/internal/core"
	"altune/overseer/internal/goapi"
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

func bucketWithSeries(t *testing.T, now func() time.Time) (*reliability.Bucket, *probes, *recordingSeries) {
	t.Helper()
	probe := newProbes(t)
	b := reliability.New(reliability.Deps{Reader: &fakeReader{}, Checker: probe, Interval: fastInterval, Now: now})
	series := &recordingSeries{}
	b.UseSeries(series)
	probe.start(b)
	return b, probe, series
}

func TestReachableProbeRecordsUpAndItsLatency(t *testing.T) {
	clock := &steppingClock{at: time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC), step: 42 * time.Millisecond}
	_, probe, series := bucketWithSeries(t, clock.now)

	probe.answer(up())

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
	if want := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC).Add(42 * time.Millisecond); !up[0].At.Equal(want) {
		t.Fatalf("probe recorded at %v, want its finish time %v", up[0].At, want)
	}
}

func TestUnreachableProbeRecordsDownWithoutALatency(t *testing.T) {
	_, probe, series := bucketWithSeries(t, nil)

	probe.answer(goapi.Health{}, srcDown("GET /health"))

	if up := series.named("up"); len(up) != 1 || up[0].Value != 0 {
		t.Fatalf("up series = %+v, want one point of 0", up)
	}
	if latency := series.named("latency_ms"); len(latency) != 0 {
		t.Fatalf("latency_ms series = %+v, want none: a probe with no answer has no latency", latency)
	}
}

func TestAnsweredDegradedProbeRecordsUpAndItsLatency(t *testing.T) {
	_, probe, series := bucketWithSeries(t, nil)

	probe.answer(degraded())

	if up := series.named("up"); len(up) != 1 || up[0].Value != 1 {
		t.Fatalf("up series = %+v, want one point of 1 (degraded is reachable)", up)
	}
	if latency := series.named("latency_ms"); len(latency) != 1 {
		t.Fatalf("latency_ms series = %+v, want one point: go-api answered", latency)
	}
}

func TestBucketIsASeriesWriter(t *testing.T) {
	var bucket core.Bucket = newBucket(&fakeReader{})
	if _, ok := bucket.(core.SeriesWriter); !ok {
		t.Fatal("reliability does not implement core.SeriesWriter, so the composition root never hands it the history store")
	}
}

func TestProbeWithoutSeriesStillPolls(t *testing.T) {
	b, probe := probedBucket(t, &fakeReader{})

	probe.answer(up())

	if got := snapData(t, b.Snapshot()).Reachability; got != "up" {
		t.Fatalf("reachability = %q, want up with no history store wired", got)
	}
}
