package auth

import metrics "altune/go-api/internal/platform/auth/internal/adapters/metrics"

type MetricsSnapshot = metrics.Snapshot

var (
	NewAuthMetrics = metrics.NewAuthMetrics
	ReadSnapshot   = metrics.ReadSnapshot
)
