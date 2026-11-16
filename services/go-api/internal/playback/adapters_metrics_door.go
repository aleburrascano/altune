package playback

import metrics "altune/go-api/internal/playback/internal/adapters/metrics"

type MetricsSnapshot = metrics.Snapshot

var (
	NewExpvarPlaybackMetrics = metrics.NewExpvarPlaybackMetrics
	ReadSnapshot             = metrics.ReadSnapshot
)
