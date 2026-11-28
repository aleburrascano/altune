package service

import (
	"strconv"
	"sync/atomic"
)

type BucketCount struct {
	LeMs  string
	Count uint64
}

type StatusClasses struct {
	Count2xx uint64
	Count4xx uint64
	Count5xx uint64
}

type RouteLatency struct {
	Count   uint64
	SumMs   uint64
	Buckets []BucketCount
	Status  StatusClasses
}

type Snapshot struct {
	Routes map[string]RouteLatency
}

func (reg *Registry) Snapshot() Snapshot {
	routes := map[string]RouteLatency{}
	reg.routes.Range(func(key, value any) bool {
		routes[key.(string)] = readRoute(value.(*routeHist))
		return true
	})
	return Snapshot{Routes: routes}
}

func readRoute(h *routeHist) RouteLatency {
	latency := h.latency.Snapshot()
	buckets := make([]BucketCount, len(latency.Buckets))
	for i, count := range latency.Buckets {
		buckets[i] = BucketCount{LeMs: bucketLabel(i), Count: count}
	}
	return RouteLatency{
		Count:   latency.Count,
		SumMs:   latency.Sum,
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
