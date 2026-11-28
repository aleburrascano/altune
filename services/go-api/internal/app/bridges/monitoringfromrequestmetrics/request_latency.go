package monitoringfromrequestmetrics

import (
	"altune/go-api/internal/platform/monitoring"
	"altune/go-api/internal/platform/requestmetrics"
)

type SnapshotSource interface {
	Snapshot() requestmetrics.Snapshot
}

type requestLatency struct {
	source SnapshotSource
}

func NewRequestLatency(source SnapshotSource) monitoring.RequestLatencyReader {
	return requestLatency{source: source}
}

func (r requestLatency) RequestLatency() monitoring.RequestLatency {
	snap := r.source.Snapshot()
	routes := make(map[string]monitoring.RouteLatency, len(snap.Routes))
	for route, rl := range snap.Routes {
		buckets := make([]monitoring.LatencyBucket, len(rl.Buckets))
		for i, b := range rl.Buckets {
			buckets[i] = monitoring.LatencyBucket{LeMs: b.LeMs, Count: b.Count}
		}
		routes[route] = monitoring.RouteLatency{
			Count: rl.Count, SumMs: rl.SumMs, Buckets: buckets,
			Status: monitoring.StatusClasses{Count2xx: rl.Status.Count2xx, Count4xx: rl.Status.Count4xx, Count5xx: rl.Status.Count5xx},
		}
	}
	return monitoring.RequestLatency{Routes: routes}
}
