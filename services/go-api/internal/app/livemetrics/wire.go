package livemetrics

import (
	"altune/go-api/internal/platform/leader"
	"altune/go-api/internal/platform/monitoring"
	"altune/go-api/internal/platform/requestmetrics"
	"time"
)

type leaderStats struct {
	Attempts  int64 `json:"acquire_attempts_total"`
	Wins      int64 `json:"acquire_wins_total"`
	Contended int64 `json:"acquire_contended_total"`
	Failures  int64 `json:"acquire_failures_total"`
	TermEnds  int64 `json:"term_ends_total"`
	HoldsLost int64 `json:"holds_lost_total"`
}

func leaderWire(c leader.Counters) leaderStats {
	return leaderStats{
		Attempts: c.Attempts, Wins: c.Wins, Contended: c.Contended,
		Failures: c.Failures, TermEnds: c.TermEnds, HoldsLost: c.HoldsLost,
	}
}

type alertMonitorStats struct {
	LastPass            time.Time `json:"last_pass"`
	LastNotifyOK        bool      `json:"last_notify_ok"`
	ConsecutiveFailures int64     `json:"consecutive_failures"`
	ContainedPanics     uint64    `json:"contained_panics"`
	NopNotifier         bool      `json:"nop_notifier"`
	LastNotifyOKAt      time.Time `json:"last_notify_ok_at"`
	LastNotifyErrorAt   time.Time `json:"last_notify_error_at"`
}

func alertMonitorWire(st monitoring.AlertStatus) alertMonitorStats {
	return alertMonitorStats{
		LastPass: st.LastPass, LastNotifyOK: st.LastNotifyOK,
		ConsecutiveFailures: st.ConsecutiveFailures, ContainedPanics: st.ContainedPanics,
		NopNotifier: st.NopNotifier, LastNotifyOKAt: st.LastNotifyOKAt, LastNotifyErrorAt: st.LastNotifyErrorAt,
	}
}

type bucketCountStats struct {
	LeMs  string `json:"le_ms"`
	Count uint64 `json:"count"`
}

type statusClassStats struct {
	Count2xx uint64 `json:"2xx"`
	Count4xx uint64 `json:"4xx"`
	Count5xx uint64 `json:"5xx"`
}

type routeLatencyStats struct {
	Count   uint64             `json:"count"`
	SumMs   uint64             `json:"sum_ms"`
	Buckets []bucketCountStats `json:"buckets"`
	Status  statusClassStats   `json:"status"`
}

type latencyStats struct {
	Routes map[string]routeLatencyStats `json:"routes"`
}

func latencyWire(snap requestmetrics.Snapshot) latencyStats {
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
