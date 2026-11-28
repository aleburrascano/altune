package livemetrics

import (
	"altune/go-api/internal/library"
	"altune/go-api/internal/platform/auth"
	"altune/go-api/internal/platform/monitoring"
	"altune/go-api/internal/platform/requestmetrics"
	"altune/go-api/internal/playback"
	"altune/go-api/internal/search"
	"altune/go-api/internal/shared/telemetry"
)

func FeatureMetrics(metrics *telemetry.Registry, latency *requestmetrics.Registry) monitoring.LiveMetrics {
	return monitoring.LiveMetrics{
		"auth":      auth.ReadSnapshot(metrics),
		"catalog":   library.ReadSnapshot(metrics),
		"playback":  playback.ReadSnapshot(metrics),
		"providers": search.ReadProviderMetricsSnapshot(metrics),
		"latency":   latencyWire(latency.Snapshot()),
	}
}
