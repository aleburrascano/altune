package handler

import (
	"altune/go-api/internal/platform/monitoring/internal/domain"
	"altune/go-api/internal/platform/monitoring/internal/ports"
	"altune/go-api/internal/platform/monitoring/internal/service"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/database"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/redis"

	goredis "github.com/redis/go-redis/v9"
)

type LiveSources struct {
	Auth         ports.SnapshotReader
	Catalog      ports.SnapshotReader
	Playback     ports.SnapshotReader
	Providers    ports.SnapshotReader
	Latency      ports.RequestLatencyReader
	Leader       ports.LeaderReader
	PoolStats    func() database.PoolStats
	Redis        *goredis.Client
	EventBus     *events.InProcessBus
	EventTap     *service.EventTap
	EventFeed    *service.EventFeed
	AlertMonitor *service.AlertMonitor
	Background   *background.Tracker
}

func NewLiveMetrics(s LiveSources) LiveMetricsSource {
	return func() LiveMetrics {
		return LiveMetrics{
			"auth":                     s.Auth.Snapshot(),
			"catalog":                  s.Catalog.Snapshot(),
			"playback":                 s.Playback.Snapshot(),
			"providers":                s.Providers.Snapshot(),
			"latency":                  latencyWire(s.Latency.RequestLatency()),
			"db_pool":                  poolStats(s.PoolStats),
			"redis_pool":               redis.ReadPoolStats(s.Redis),
			"leader":                   leaderWire(s.Leader.LeaderCounters()),
			"event_bus":                eventBusStats{Dropped: s.EventBus.Dropped()},
			"alert_monitor":            alertMonitorWire(s.AlertMonitor.Status()),
			"event_tap":                eventTapStats{TapDropped: s.EventTap.Dropped(), FeedDropped: s.EventFeed.Dropped()},
			"background_tasks_dropped": s.Background.Dropped(),
		}
	}
}

func poolStats(source func() database.PoolStats) database.PoolStats {
	if source == nil {
		return database.PoolStats{}
	}
	return source()
}

func leaderWire(c domain.LeaderCounters) leaderStats {
	return leaderStats{
		Attempts: c.Attempts, Wins: c.Wins, Contended: c.Contended,
		Failures: c.Failures, TermEnds: c.TermEnds, HoldsLost: c.HoldsLost,
	}
}

func alertMonitorWire(st service.AlertStatus) alertMonitorStats {
	return alertMonitorStats{
		LastPass: st.LastPass, LastNotifyOK: st.LastNotifyOK,
		ConsecutiveFailures: st.ConsecutiveFailures, ContainedPanics: st.ContainedPanics,
		NopNotifier: st.NopNotifier, LastNotifyOKAt: st.LastNotifyOKAt, LastNotifyErrorAt: st.LastNotifyErrorAt,
	}
}

func latencyWire(snap domain.RequestLatency) latencyStats {
	routes := make(map[string]routeLatencyStats, len(snap.Routes))
	for route, rl := range snap.Routes {
		buckets := make([]bucketCountStats, len(rl.Buckets))
		for i, b := range rl.Buckets {
			buckets[i] = bucketCountStats{LeMs: b.LeMs, Count: b.Count}
		}
		routes[route] = routeLatencyStats{
			Count: rl.Count, SumMs: rl.SumMs, Buckets: buckets,
			Status: statusClassStats{Count2xx: rl.Status.Count2xx, Count4xx: rl.Status.Count4xx, Count5xx: rl.Status.Count5xx},
		}
	}
	return latencyStats{Routes: routes}
}
