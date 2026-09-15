package backendperf

import (
	"altune/overseer/internal/goapi"
	"math"
	"testing"
)

// stdBounds mirrors go-api's fixed histogram bucket labels (reqmetrics), oldest
// bound first, with the unbounded +Inf tail last. Tests build a distribution by
// naming only the non-zero buckets, keeping the order the estimator depends on.
var stdBounds = []string{"1", "5", "10", "25", "50", "100", "250", "500", "1000", "2500", "5000", "+Inf"}

// hist builds an ordered bucket slice from a label->count map, filling unnamed
// buckets with zero. The slice order matches go-api's, which the estimator relies
// on to walk the cumulative distribution.
func hist(counts map[string]uint64) []goapi.LatencyBucket {
	out := make([]goapi.LatencyBucket, len(stdBounds))
	for i, label := range stdBounds {
		out[i] = goapi.LatencyBucket{LeMs: label, Count: counts[label]}
	}
	return out
}

func approxEq(got, want float64) bool { return math.Abs(got-want) < 0.01 }

// TestEstimatePercentileInterpolatesWithinBucket is the core estimation proof:
// with every request in the (5,10] bucket, p50/p95/p99 land at the interpolated
// positions inside that bucket, not at a bucket edge.
func TestEstimatePercentileInterpolatesWithinBucket(t *testing.T) {
	buckets := hist(map[string]uint64{"10": 100})

	cases := []struct {
		p    float64
		want float64
	}{
		{0.50, 7.5},  // 5 + 0.50*(10-5)
		{0.95, 9.75}, // 5 + 0.95*(10-5)
		{0.99, 9.95}, // 5 + 0.99*(10-5)
	}
	for _, c := range cases {
		got := estimatePercentile(buckets, c.p)
		if got.Overflow {
			t.Errorf("p%.0f flagged overflow inside a finite bucket", c.p*100)
		}
		if !approxEq(got.Ms, c.want) {
			t.Errorf("p%.0f = %.4fms, want %.4fms", c.p*100, got.Ms, c.want)
		}
	}
}

// TestEstimatePercentileSpansBuckets proves the estimator walks the cumulative
// distribution across buckets: with a bimodal split the median falls in the fast
// bucket and the tail percentile in the slow one.
func TestEstimatePercentileSpansBuckets(t *testing.T) {
	// 90 fast requests in (5,10], 10 slow in (100,250].
	buckets := hist(map[string]uint64{"10": 90, "250": 10})

	p50 := estimatePercentile(buckets, 0.50)
	if p50.Overflow || p50.Ms < 5 || p50.Ms > 10 {
		t.Errorf("p50 = %+v, want a value inside (5,10]", p50)
	}
	// rank(p99) = 99 → past the 90 fast, into the slow bucket (100,250].
	p99 := estimatePercentile(buckets, 0.99)
	if p99.Overflow || p99.Ms <= 100 || p99.Ms > 250 {
		t.Errorf("p99 = %+v, want a value inside (100,250]", p99)
	}
}

// TestEstimatePercentileOverflowTail proves a percentile landing in the unbounded
// +Inf tail is returned as a lower bound flagged Overflow — it can only be
// bounded below, never interpolated.
func TestEstimatePercentileOverflowTail(t *testing.T) {
	buckets := hist(map[string]uint64{"+Inf": 100})

	got := estimatePercentile(buckets, 0.99)
	if !got.Overflow {
		t.Fatalf("p99 in the +Inf tail = %+v, want Overflow", got)
	}
	if got.Ms != 5000 {
		t.Errorf("overflow lower bound = %.0fms, want the last finite bound 5000ms", got.Ms)
	}
}

// TestEstimatePercentileEmptyIsZero proves a route with no observed requests
// yields a zero-value estimate rather than dividing by zero.
func TestEstimatePercentileEmptyIsZero(t *testing.T) {
	got := estimatePercentile(hist(nil), 0.50)
	if got.Ms != 0 || got.Overflow {
		t.Errorf("empty histogram estimate = %+v, want zero value", got)
	}
}

// TestRouteStatsDropsEmptyAndSortsSlowestFirst proves routeStats drops zero-count
// routes and orders the rest slowest-first by p99, so the slowest route heads the
// panel.
func TestRouteStatsDropsEmptyAndSortsSlowestFirst(t *testing.T) {
	m := goapi.LatencyMetrics{Routes: map[string]goapi.RouteLatency{
		"/fast":  {Count: 100, Buckets: hist(map[string]uint64{"10": 100})},
		"/slow":  {Count: 100, Buckets: hist(map[string]uint64{"1000": 100})},
		"/empty": {Count: 0, Buckets: hist(nil)},
	}}

	stats := routeStats(m)
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
