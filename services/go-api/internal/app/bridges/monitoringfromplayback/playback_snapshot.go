package monitoringfromplayback

import (
	"altune/go-api/internal/platform/monitoring"
	"altune/go-api/internal/playback"
	"altune/go-api/internal/shared/telemetry"
)

type playbackSnapshot struct {
	metrics *telemetry.Registry
}

func NewSnapshot(metrics *telemetry.Registry) monitoring.SnapshotReader {
	return playbackSnapshot{metrics: metrics}
}

func (p playbackSnapshot) Snapshot() any {
	return playback.ReadSnapshot(p.metrics)
}
