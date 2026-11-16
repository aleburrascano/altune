package catalog

import metrics "altune/go-api/internal/catalog/internal/adapters/metrics"

type MetricsSnapshot = metrics.Snapshot

var (
	NewExpvarAudioStoreMetrics = metrics.NewExpvarAudioStoreMetrics
	NewExpvarDBCallMetrics     = metrics.NewExpvarDBCallMetrics
	ReadSnapshot               = metrics.ReadSnapshot
)
