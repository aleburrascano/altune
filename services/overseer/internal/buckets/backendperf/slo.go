package backendperf

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/goapi"
	"time"
)

const (
	searchRoute          = "/v1/discovery/search"
	searchLatencyBoundMs = 1000.0
)

type searchSLOs struct {
	availability *core.SLOWindow
	latency      *core.SLOWindow
}

func newSearchSLOs() searchSLOs {
	return searchSLOs{
		availability: core.NewSLOWindow(core.SLO{Journey: "find music", Name: "search answers without a server error", Target: 0.995}),
		latency:      core.NewSLOWindow(core.SLO{Journey: "find music", Name: "search answers within 1 s", Target: 0.95}),
	}
}

func (s searchSLOs) record(at time.Time, rl goapi.RouteLatency) {
	classified := classifiedResponses(rl.Status)
	s.availability.Record(at, classified-min(rl.Status.Count5xx, classified), classified)
	s.latency.Record(at, bucketsWithin(rl.Buckets, searchLatencyBoundMs), bucketTotal(rl.Buckets))
}

func (s searchSLOs) statuses(now time.Time) []core.SLOStatus {
	return []core.SLOStatus{s.availability.Status(now), s.latency.Status(now)}
}

func bucketsWithin(buckets []goapi.LatencyBucket, boundMs float64) uint64 {
	var within uint64
	for _, b := range buckets {
		if bucketBound(b.LeMs) <= boundMs {
			within += b.Count
		}
	}
	return within
}
