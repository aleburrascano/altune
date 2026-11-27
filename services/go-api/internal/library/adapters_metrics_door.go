package library

import metrics "altune/go-api/internal/library/internal/adapters/metrics"

type MetricsSnapshot = metrics.Snapshot

var (
	NewAudioStoreMetrics = metrics.NewAudioStoreMetrics
	NewDBCallMetrics     = metrics.NewDBCallMetrics
	NewFSCallMetrics     = metrics.NewFSCallMetrics
	ReadSnapshot         = metrics.ReadSnapshot
)
