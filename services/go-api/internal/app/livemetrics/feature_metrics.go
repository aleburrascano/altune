package livemetrics

import (
	"altune/go-api/internal/library"
	"altune/go-api/internal/platform/auth"
	"altune/go-api/internal/platform/monitoring"
	"altune/go-api/internal/platform/requestmetrics"
	"altune/go-api/internal/playback"
	"altune/go-api/internal/search"
)

func FeatureMetrics(latency *requestmetrics.Registry) monitoring.LiveMetrics {
	return monitoring.LiveMetrics{
		"auth":      auth.ReadSnapshot(),
		"catalog":   library.ReadSnapshot(),
		"playback":  playback.ReadSnapshot(),
		"providers": search.ReadProviderMetricsSnapshot(),
		"latency":   latency.Snapshot(),
	}
}
