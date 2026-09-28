package backendperf

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

func TestEstimatePercentileInterpolatesWithinBucket(t *testing.T) {
	buckets := hist(map[string]uint64{"10": 100})

	cases := []struct {
		p    float64
		want float64
	}{
		{0.50, 7.5},
		{0.95, 9.75},
		{0.99, 9.95},
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

func TestEstimatePercentileSpansBuckets(t *testing.T) {
	buckets := hist(map[string]uint64{"10": 90, "250": 10})

	p50 := estimatePercentile(buckets, 0.50)
	if p50.Overflow || p50.Ms < 5 || p50.Ms > 10 {
		t.Errorf("p50 = %+v, want a value inside (5,10]", p50)
	}
	p99 := estimatePercentile(buckets, 0.99)
	if p99.Overflow || p99.Ms <= 100 || p99.Ms > 250 {
		t.Errorf("p99 = %+v, want a value inside (100,250]", p99)
	}
}

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

func TestEstimatePercentileEmptyIsZero(t *testing.T) {
	got := estimatePercentile(hist(nil), 0.50)
	if got.Ms != 0 || got.Overflow {
		t.Errorf("empty histogram estimate = %+v, want zero value", got)
	}
}

func TestEstimatePercentileNonFiniteBoundDegradesToOverflow(t *testing.T) {
	buckets := []goapi.LatencyBucket{{LeMs: "NaN", Count: 100}}

	got := estimatePercentile(buckets, 0.99)
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
