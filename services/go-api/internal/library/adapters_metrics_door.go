package library

import metrics "altune/go-api/internal/library/internal/adapters/metrics"

type MetricsSnapshot = metrics.Snapshot

var (
	NewExpvarAudioStoreMetrics = metrics.NewExpvarAudioStoreMetrics
	NewExpvarDBCallMetrics     = metrics.NewExpvarDBCallMetrics
	ReadSnapshot               = metrics.ReadSnapshot
)
