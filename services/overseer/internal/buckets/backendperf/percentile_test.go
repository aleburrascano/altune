package backendperf_test

import (
	"altune/overseer/internal/goapi"
	"math"
	"testing"
)

var stdBounds = []string{"1", "5", "10", "25", "50", "100", "250", "500", "1000", "2500", "5000", "+Inf"}

func hist(counts map[string]uint64) []goapi.LatencyBucket {
	out := make([]goapi.LatencyBucket, len(stdBounds))
	for i, label := range stdBounds {
		out[i] = goapi.LatencyBucket{LeMs: label, Count: counts[label]}
	}
	return out
}

func approxEq(got, want float64) bool { return math.Abs(got-want) < 0.01 }

type estimate struct {
	Ms       float64
	Overflow bool
}

func percentilesOf(t *testing.T, buckets []goapi.LatencyBucket) map[float64]estimate {
	t.Helper()
	var total uint64
	for _, b := range buckets {
		total += b.Count
	}
	reader := &fakeReader{}
	reader.set(liveWith(map[string]goapi.RouteLatency{"/r": {Count: total, Buckets: buckets}}), nil)
	b := newBucket(reader)
	if err := collectStore(t, b); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	d := snapData(t, b.Snapshot())
	if len(d.Routes) != 1 {
		t.Fatalf("routes = %+v, want the one route", d.Routes)
	}
	r := d.Routes[0]
	return map[float64]estimate{
		0.50: {Ms: r.P50.Ms, Overflow: r.P50.Overflow},
		0.95: {Ms: r.P95.Ms, Overflow: r.P95.Overflow},
		0.99: {Ms: r.P99.Ms, Overflow: r.P99.Overflow},
	}
}

func TestEstimatePercentileInterpolatesWithinBucket(t *testing.T) {
	got := percentilesOf(t, hist(map[string]uint64{"10": 100}))

	for p, want := range map[float64]float64{0.50: 7.5, 0.95: 9.75, 0.99: 9.95} {
		if got[p].Overflow {
			t.Errorf("p%.0f flagged overflow inside a finite bucket", p*100)
		}
		if !approxEq(got[p].Ms, want) {
			t.Errorf("p%.0f = %.4fms, want %.4fms", p*100, got[p].Ms, want)
		}
	}
}

func TestEstimatePercentileSpansBuckets(t *testing.T) {
	got := percentilesOf(t, hist(map[string]uint64{"10": 90, "250": 10}))

	if p50 := got[0.50]; p50.Overflow || p50.Ms < 5 || p50.Ms > 10 {
		t.Errorf("p50 = %+v, want a value inside (5,10]", p50)
	}
	if p99 := got[0.99]; p99.Overflow || p99.Ms <= 100 || p99.Ms > 250 {
		t.Errorf("p99 = %+v, want a value inside (100,250]", p99)
	}
}

func TestEstimatePercentileOverflowTail(t *testing.T) {
	got := percentilesOf(t, hist(map[string]uint64{"+Inf": 100}))[0.99]

	if !got.Overflow {
		t.Fatalf("p99 in the +Inf tail = %+v, want Overflow", got)
	}
	if got.Ms != 5000 {
		t.Errorf("overflow lower bound = %.0fms, want the last finite bound 5000ms", got.Ms)
	}
}

func TestEstimatePercentileEmptyIsZero(t *testing.T) {
	reader := &fakeReader{}
	reader.set(liveWith(map[string]goapi.RouteLatency{"/idle": {Buckets: hist(nil)}}), nil)
	b, series := bucketWithSeries(reader)
	if err := collectStore(t, b); err != nil {
		t.Fatalf("Collect: %v", err)
	}

	for _, name := range []string{"p50_ms", "p95_ms", "p99_ms"} {
		points := series.named(name)
		if len(points) != 1 || points[0].Value != 0 {
			t.Errorf("%s over an empty histogram = %+v, want one point of 0", name, points)
		}
	}
}

func TestEstimatePercentileNonFiniteBoundDegradesToOverflow(t *testing.T) {
	got := percentilesOf(t, []goapi.LatencyBucket{{LeMs: "NaN", Count: 100}})[0.99]

	if math.IsNaN(got.Ms) {
		t.Fatalf("NaN bound poisoned the estimate: %+v", got)
	}
	if !got.Overflow {
		t.Errorf("NaN bound estimate = %+v, want the unbounded overflow tail", got)
	}
	if got.Ms != 0 {
		t.Errorf("NaN-bound overflow lower bound = %v, want 0", got.Ms)
	}
}

func TestRouteStatsDropsEmptyAndSortsSlowestFirst(t *testing.T) {
	reader := &fakeReader{}
	reader.set(liveWith(map[string]goapi.RouteLatency{
		"/fast":  {Count: 100, Buckets: hist(map[string]uint64{"10": 100})},
		"/slow":  {Count: 100, Buckets: hist(map[string]uint64{"1000": 100})},
		"/empty": {Count: 0, Buckets: hist(nil)},
	}), nil)
	b := newBucket(reader)
	if err := collectStore(t, b); err != nil {
		t.Fatalf("Collect: %v", err)
	}

	stats := snapData(t, b.Snapshot()).Routes
	if len(stats) != 2 {
		t.Fatalf("got %d stats, want 2 (the empty route dropped)", len(stats))
	}
	if stats[0].Route != "/slow" {
		t.Errorf("slowest-first head = %q, want /slow", stats[0].Route)
	}
	if stats[1].Route != "/fast" {
		t.Errorf("second = %q, want /fast", stats[1].Route)
	}
	if stats[0].P99.Ms <= stats[1].P99.Ms {
		t.Errorf("p99 ordering wrong: %.2f !> %.2f", stats[0].P99.Ms, stats[1].P99.Ms)
	}
}
