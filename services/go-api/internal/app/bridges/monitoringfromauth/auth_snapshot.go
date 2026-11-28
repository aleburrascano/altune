package monitoringfromauth

import (
	"altune/go-api/internal/platform/auth"
	"altune/go-api/internal/platform/monitoring"
	"altune/go-api/internal/shared/telemetry"
)

type authSnapshot struct {
	metrics *telemetry.Registry
}

func NewSnapshot(metrics *telemetry.Registry) monitoring.SnapshotReader {
	return authSnapshot{metrics: metrics}
}

func (a authSnapshot) Snapshot() any {
	return auth.ReadSnapshot(a.metrics)
}
