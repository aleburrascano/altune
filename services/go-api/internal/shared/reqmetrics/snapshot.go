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

// StatusClasses is one route's 2xx/4xx/5xx response tally, shaped for JSON.
// Statuses outside these classes are not counted here.
type StatusClasses struct {
	Count2xx uint64 `json:"2xx"`
	Count4xx uint64 `json:"4xx"`
	Count5xx uint64 `json:"5xx"`
}

// RouteLatency is one route's latency distribution and status-class tally,
// shaped for JSON.
type RouteLatency struct {
	Count   uint64        `json:"count"`
	SumMs   uint64        `json:"sum_ms"`
	Buckets []BucketCount `json:"buckets"`
	Status  StatusClasses `json:"status"`
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
		Status: StatusClasses{
			Count2xx: atomic.LoadUint64(&h.statusClass[class2xx]),
			Count4xx: atomic.LoadUint64(&h.statusClass[class4xx]),
			Count5xx: atomic.LoadUint64(&h.statusClass[class5xx]),
		},
	}
}

func bucketLabel(i int) string {
	if i >= len(bucketUpperBoundsMs) {
		return "+Inf"
	}
	return strconv.FormatInt(bucketUpperBoundsMs[i], 10)
}
