package auth

import metrics "altune/go-api/internal/auth/internal/adapters/metrics"

type MetricsSnapshot = metrics.Snapshot

var (
	NewExpvarAuthMetrics = metrics.NewExpvarAuthMetrics
	ReadSnapshot         = metrics.ReadSnapshot
)
