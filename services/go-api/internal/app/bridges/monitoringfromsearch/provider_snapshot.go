package monitoringfromsearch

import (
	"altune/go-api/internal/platform/monitoring"
	"altune/go-api/internal/search"
	"altune/go-api/internal/shared/telemetry"
)

type providerSnapshot struct {
	metrics *telemetry.Registry
}

func NewProviderSnapshot(metrics *telemetry.Registry) monitoring.SnapshotReader {
	return providerSnapshot{metrics: metrics}
}

func (p providerSnapshot) Snapshot() any {
	return search.ReadProviderMetricsSnapshot(p.metrics)
}
