package playback

import metrics "altune/go-api/internal/playback/internal/adapters/metrics"

type MetricsSnapshot = metrics.Snapshot

var (
	NewPlaybackMetrics = metrics.NewPlaybackMetrics
	ReadSnapshot       = metrics.ReadSnapshot
)
