package backendperf

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/goapi"
	"context"
	"fmt"
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
		if p.bucket == bucketID && p.series == series {
			out = append(out, p.point)
		}
	}
	return out
}

func (r *recordingSeries) count() map[string]int {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := map[string]int{}
	for _, p := range r.points {
		out[p.series]++
	}
	return out
}

func bucketWithSeries(reader metricsReader) (*Bucket, *recordingSeries) {
	b := newBucket(reader)
	series := &recordingSeries{}
	b.UseSeries(series)
	return b, series
}

func TestCollectRecordsEachOverallSeriesOnce(t *testing.T) {
	reader := &fakeReader{}
	reader.set(liveWith(map[string]goapi.RouteLatency{
		"/v1/library": {
			Count:   100,
			Buckets: hist(map[string]uint64{"10": 100}),
			Status:  goapi.StatusClasses{Count2xx: 95, Count5xx: 5},
		},
	}), nil)
	b, series := bucketWithSeries(reader)

	if err := collectStore(t, b); err != nil {
		t.Fatalf("Collect: %v", err)
	}

	for _, name := range []string{seriesThroughput, seriesErrorRate, seriesP50MS, seriesP95MS, seriesP99MS} {
		got := series.named(name)
		if len(got) != 1 {
			t.Fatalf("series %q recorded %d points, want 1: %+v", name, len(got), got)
		}
	}
	errorRatePoints := series.named(seriesErrorRate)
	if len(errorRatePoints) == 1 && errorRatePoints[0].Value != 0.05 {
		t.Errorf("error_rate = %v, want 0.05", errorRatePoints[0].Value)
	}
	p99Points := series.named(seriesP99MS)
	if len(p99Points) == 1 && p99Points[0].Value <= 0 {
		t.Errorf("p99_ms = %v, want a positive estimate", p99Points[0].Value)
	}
}

func TestCollectRecordsOnePerRouteSeriesPerRoute(t *testing.T) {
	reader := &fakeReader{}
	reader.set(liveWith(map[string]goapi.RouteLatency{
		"/a": {Count: 100, Buckets: hist(map[string]uint64{"10": 100})},
		"/b": {Count: 50, Buckets: hist(map[string]uint64{"25": 50})},
	}), nil)
	b, series := bucketWithSeries(reader)

	if err := collectStore(t, b); err != nil {
		t.Fatalf("Collect: %v", err)
	}

	if got := series.named(perRouteSeriesStem + "/a"); len(got) != 1 {
		t.Errorf("p95_ms:/a recorded %d points, want 1: %+v", len(got), got)
	}
	if got := series.named(perRouteSeriesStem + "/b"); len(got) != 1 {
		t.Errorf("p95_ms:/b recorded %d points, want 1: %+v", len(got), got)
	}
}

func TestCollectNeverRecordsMoreThanMaxPerRouteSeries(t *testing.T) {
	routes := make(map[string]goapi.RouteLatency, maxPerRouteSeries+15)
	for i := 0; i < maxPerRouteSeries+15; i++ {
		routes[fmt.Sprintf("/route-%02d", i)] = goapi.RouteLatency{
			Count:   uint64(i + 1),
			Buckets: hist(map[string]uint64{"10": uint64(i + 1)}),
		}
	}
	reader := &fakeReader{}
	reader.set(liveWith(routes), nil)
	b, series := bucketWithSeries(reader)

	if err := collectStore(t, b); err != nil {
		t.Fatalf("Collect: %v", err)
	}

	perRoute := 0
	for name, n := range series.count() {
		if len(name) > len(perRouteSeriesStem) && name[:len(perRouteSeriesStem)] == perRouteSeriesStem {
			perRoute += n
		}
	}
	if perRoute > 20 {
		t.Errorf("per-route series recorded = %d, want at most 20", perRoute)
	}
}

func TestCollectKeepsTheBusiestRoutesWhenCapped(t *testing.T) {
	routes := map[string]goapi.RouteLatency{
		"/busy": {Count: 1000, Buckets: hist(map[string]uint64{"10": 1000})},
		"/idle": {Count: 1, Buckets: hist(map[string]uint64{"10": 1})},
	}
	for i := 0; i < 20; i++ {
		routes[fmt.Sprintf("/mid-%02d", i)] = goapi.RouteLatency{
			Count:   10,
			Buckets: hist(map[string]uint64{"10": 10}),
		}
	}
	reader := &fakeReader{}
	reader.set(liveWith(routes), nil)
	b, series := bucketWithSeries(reader)

	if err := collectStore(t, b); err != nil {
		t.Fatalf("Collect: %v", err)
	}

	if got := series.named(perRouteSeriesStem + "/busy"); len(got) != 1 {
		t.Errorf("busiest route dropped from per-route series: %+v", got)
	}
	if got := series.named(perRouteSeriesStem + "/idle"); len(got) != 0 {
		t.Errorf("idle route recorded a per-route series despite the cap: %+v", got)
	}
}

func TestKeySeriesIsP95MS(t *testing.T) {
	b := newBucket(&fakeReader{})
	if got := b.KeySeries(); got != seriesP95MS {
		t.Errorf("KeySeries() = %q, want %q", got, seriesP95MS)
	}
}

func TestBucketIsASeriesWriter(t *testing.T) {
	var bucket core.Bucket = newBucket(&fakeReader{})
	if _, ok := bucket.(core.SeriesWriter); !ok {
		t.Fatal("backendperf does not implement core.SeriesWriter, so the composition root never hands it the history store")
	}
}

func TestCollectWithoutSeriesStillCollects(t *testing.T) {
	reader := &fakeReader{}
	reader.set(liveWith(map[string]goapi.RouteLatency{
		"/a": {Count: 1, Buckets: hist(map[string]uint64{"10": 1})},
	}), nil)
	b := newBucket(reader)

	if _, err := b.Collect(context.Background()); err != nil {
		t.Fatalf("Collect with no series wired: %v", err)
	}
}
