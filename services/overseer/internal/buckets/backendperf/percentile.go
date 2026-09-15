package backendperf

import (
	"altune/overseer/internal/goapi"
	"math"
	"sort"
	"strconv"
)

// routeStat is one route's estimated latency profile for the panel: its request
// count (throughput since go-api start) and p50/p95/p99 latency estimates. It is
// derived from the route's fixed histogram buckets, never from raw samples.
type routeStat struct {
	Route string
	Count uint64
	P50   percentile
	P95   percentile
	P99   percentile
}

// percentile is one estimated latency in milliseconds. Overflow is true when the
// estimate fell in the histogram's unbounded "+Inf" tail, where the value can
// only be a lower bound — the panel marks such an estimate so it is never read
// as exact.
type percentile struct {
	Ms       float64
	Overflow bool
}

// routeStats estimates each route's p50/p95/p99 from its histogram buckets and
// returns them sorted slowest-first (by p99, then throughput, then name) so the
// slowest routes surface at the top of the panel. A route with no observed
// requests is dropped: a percentile of nothing is meaningless.
func routeStats(m goapi.LatencyMetrics) []routeStat {
	stats := make([]routeStat, 0, len(m.Routes))
	for route, rl := range m.Routes {
		if bucketTotal(rl.Buckets) == 0 {
			continue
		}
		stats = append(stats, statFor(route, rl))
	}
	sortSlowestFirst(stats)
	return stats
}

func statFor(route string, rl goapi.RouteLatency) routeStat {
	return routeStat{
		Route: route,
		Count: rl.Count,
		P50:   estimatePercentile(rl.Buckets, 0.50),
		P95:   estimatePercentile(rl.Buckets, 0.95),
		P99:   estimatePercentile(rl.Buckets, 0.99),
	}
}

// sortSlowestFirst orders routes by p99 descending, breaking ties by throughput
// then route name so the ordering is deterministic (a stable panel across reads).
func sortSlowestFirst(stats []routeStat) {
	sort.SliceStable(stats, func(i, j int) bool {
		a, b := stats[i], stats[j]
		if a.P99.Ms != b.P99.Ms {
			return a.P99.Ms > b.P99.Ms
		}
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		return a.Route < b.Route
	})
}

// estimatePercentile estimates the p-quantile latency from fixed histogram
// buckets by linear interpolation within the bucket the quantile falls in. The
// buckets carry per-bucket (non-cumulative) counts with inclusive upper bounds;
// a quantile landing in the unbounded +Inf tail can only be bounded below, so it
// returns that tail's lower bound flagged Overflow. Accuracy is bounded by the
// bucket widths — acceptable for an operator view, per the brief.
func estimatePercentile(buckets []goapi.LatencyBucket, p float64) percentile {
	total := bucketTotal(buckets)
	if total == 0 {
		return percentile{}
	}
	rank := p * float64(total)
	var cum float64
	lower := 0.0
	for _, b := range buckets {
		upper := bucketBound(b.LeMs)
		if b.Count > 0 && cum+float64(b.Count) >= rank {
			return interpolate(lower, upper, cum, float64(b.Count), rank)
		}
		cum += float64(b.Count)
		if !math.IsInf(upper, 1) {
			lower = upper
		}
	}
	return percentile{Ms: lower, Overflow: true}
}

// interpolate places rank linearly within [lower, upper] given the bucket's
// count and the cumulative count before it. An unbounded upper (the +Inf tail)
// admits only a lower bound, returned flagged Overflow.
func interpolate(lower, upper, cum, count, rank float64) percentile {
	if math.IsInf(upper, 1) {
		return percentile{Ms: lower, Overflow: true}
	}
	frac := (rank - cum) / count
	return percentile{Ms: lower + frac*(upper-lower)}
}

// bucketTotal sums the per-bucket counts: the route's total observed requests.
func bucketTotal(buckets []goapi.LatencyBucket) uint64 {
	var total uint64
	for _, b := range buckets {
		total += b.Count
	}
	return total
}

// bucketBound parses a bucket's le_ms label into its numeric upper bound in
// milliseconds. The final "+Inf" label — and, defensively, any unparseable or
// non-finite (NaN) label a skewed or hostile go-api might send — is treated as
// unbounded, so a garbage bound degrades to the overflow tail rather than
// poisoning the interpolation arithmetic (a NaN bound would render "NaNms").
func bucketBound(label string) float64 {
	v, err := strconv.ParseFloat(label, 64)
	if err != nil || math.IsNaN(v) {
		return math.Inf(1)
	}
	return v
}
