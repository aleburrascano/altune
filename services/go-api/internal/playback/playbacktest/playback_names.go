package playbacktest

import (
	"altune/go-api/internal/playback/internal/adapters/handler"
	"altune/go-api/internal/playback/internal/adapters/telemetry"
	"altune/go-api/internal/playback/internal/domain"
)

type MetricsSnapshot = telemetry.Snapshot

var (
	ErrIdentityStoreUnavailable = domain.ErrIdentityStoreUnavailable
	DefaultQueueSaveLimit       = handler.DefaultQueueSaveLimit
)
