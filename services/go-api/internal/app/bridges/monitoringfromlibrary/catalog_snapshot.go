package monitoringfromlibrary

import (
	"altune/go-api/internal/library"
	"altune/go-api/internal/platform/monitoring"
	"altune/go-api/internal/shared/telemetry"
)

type catalogSnapshot struct {
	metrics *telemetry.Registry
}

func NewSnapshot(metrics *telemetry.Registry) monitoring.SnapshotReader {
	return catalogSnapshot{metrics: metrics}
}

func (c catalogSnapshot) Snapshot() any {
	return library.ReadSnapshot(c.metrics)
}
