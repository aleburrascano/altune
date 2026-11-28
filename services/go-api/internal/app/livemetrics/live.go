package livemetrics

import (
	"altune/go-api/internal/platform/leader"
	"altune/go-api/internal/platform/monitoring"
	"altune/go-api/internal/platform/requestmetrics"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/database"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/redis"
	"altune/go-api/internal/shared/telemetry"

	goredis "github.com/redis/go-redis/v9"
)

type Sources struct {
	Latency      *requestmetrics.Registry
	Metrics      *telemetry.Registry
	PoolStats    func() database.PoolStats
	Redis        *goredis.Client
	Leader       leader.Counters
	EventBus     *events.InProcessBus
	EventTap     *monitoring.EventTap
	EventFeed    *monitoring.EventFeed
	AlertMonitor *monitoring.AlertMonitor
	Background   *background.Tracker
}

type eventBusStats struct {
	Dropped uint64 `json:"dropped_total"`
}

type eventTapStats struct {
	TapDropped  uint64 `json:"tap_dropped_total"`
	FeedDropped uint64 `json:"feed_dropped_total"`
}

func Live(s Sources) monitoring.LiveMetrics {
	m := FeatureMetrics(s.Metrics, s.Latency)
	m["db_pool"] = poolStats(s.PoolStats)
	m["redis_pool"] = redis.ReadPoolStats(s.Redis)
	m["leader"] = leaderWire(s.Leader)
	m["event_bus"] = eventBusStats{Dropped: s.EventBus.Dropped()}
	m["alert_monitor"] = alertMonitorWire(s.AlertMonitor.Status())
	m["event_tap"] = eventTapStats{TapDropped: s.EventTap.Dropped(), FeedDropped: s.EventFeed.Dropped()}
	m["background_tasks_dropped"] = s.Background.Dropped()
	return m
}

func poolStats(source func() database.PoolStats) database.PoolStats {
	if source == nil {
		return database.PoolStats{}
	}
	return source()
}
