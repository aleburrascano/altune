package observemetrics

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/catalog"
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/observe"
	"altune/go-api/internal/playback"
	"altune/go-api/internal/shared/reqmetrics"
)

func FeatureMetrics() observe.LiveMetrics {
	return observe.LiveMetrics{
		"auth":      auth.ReadSnapshot(),
		"catalog":   catalog.ReadSnapshot(),
		"playback":  playback.ReadSnapshot(),
		"providers": discovery.ReadProviderMetricsSnapshot(),
		"latency":   reqmetrics.ReadSnapshot(),
	}
}
