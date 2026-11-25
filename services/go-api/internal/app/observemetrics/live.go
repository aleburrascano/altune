package observemetrics

import (
	"altune/go-api/internal/observe"
	"altune/go-api/internal/shared/database"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/leader"
	"altune/go-api/internal/shared/redis"
	"altune/go-api/internal/shared/reqmetrics"
	"altune/go-api/internal/shared/runloop"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
)

type Sources struct {
	Latency      *reqmetrics.Registry
	Pool         *pgxpool.Pool
	Redis        *goredis.Client
	Leader       leader.Counters
	EventBus     *events.InProcessBus
	EventTap     *observe.EventTap
	EventFeed    *observe.EventFeed
	AlertMonitor *observe.AlertMonitor
	Background   *runloop.Tracker
}

type eventBusStats struct {
	Dropped uint64 `json:"dropped_total"`
}

type eventTapStats struct {
	TapDropped  uint64 `json:"tap_dropped_total"`
	FeedDropped uint64 `json:"feed_dropped_total"`
}

func Live(s Sources) observe.LiveMetrics {
	m := FeatureMetrics(s.Latency)
	m["db_pool"] = database.ReadPoolStats(s.Pool)
	m["redis_pool"] = redis.ReadPoolStats(s.Redis)
	m["leader"] = s.Leader
	m["event_bus"] = eventBusStats{Dropped: s.EventBus.Dropped()}
	m["alert_monitor"] = s.AlertMonitor.Status()
	m["event_tap"] = eventTapStats{TapDropped: s.EventTap.Dropped(), FeedDropped: s.EventFeed.Dropped()}
	m["background_tasks_dropped"] = s.Background.Dropped()
	return m
}
