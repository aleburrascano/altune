package reqmetrics

import (
	"strconv"
	"sync/atomic"
)

// BucketCount is one histogram bucket in a Snapshot: the count of requests whose
// latency was at or below LeMs milliseconds. The final bucket carries the label
// "+Inf" and holds everything slower than the last fixed bound.
type BucketCount struct {
	LeMs  string `json:"le_ms"`
	Count uint64 `json:"count"`
}

// RouteLatency is one route's latency distribution, shaped for JSON.
type RouteLatency struct {
	Count   uint64        `json:"count"`
	SumMs   uint64        `json:"sum_ms"`
	Buckets []BucketCount `json:"buckets"`
}

// Snapshot is a point-in-time read of the per-route latency histograms.
type Snapshot struct {
	Routes map[string]RouteLatency `json:"routes"`
}

// ReadSnapshot returns the current per-route latency distributions. It reads the
// atomic counters without blocking recording, so counts may be marginally
// inconsistent across routes — acceptable for an operator metrics view.
func ReadSnapshot() Snapshot {
	routes := map[string]RouteLatency{}
	defaultRegistry.routes.Range(func(key, value any) bool {
		routes[key.(string)] = readRoute(value.(*routeHist))
		return true
	})
	return Snapshot{Routes: routes}
}

func readRoute(h *routeHist) RouteLatency {
	buckets := make([]BucketCount, numBuckets)
	for i := range buckets {
		buckets[i] = BucketCount{LeMs: bucketLabel(i), Count: atomic.LoadUint64(&h.buckets[i])}
	}
	return RouteLatency{
		Count:   atomic.LoadUint64(&h.count),
		SumMs:   atomic.LoadUint64(&h.sumMs),
		Buckets: buckets,
	}
}

func bucketLabel(i int) string {
	if i >= len(bucketUpperBoundsMs) {
		return "+Inf"
	}
	return strconv.FormatInt(bucketUpperBoundsMs[i], 10)
}
