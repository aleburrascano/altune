package playback

import persistence "altune/go-api/internal/playback/internal/adapters/persistence"

var (
	NewPgxDeletedIdentityRepository = persistence.NewPgxDeletedIdentityRepository
	NewPgxQueueStateRepository      = persistence.NewPgxQueueStateRepository
	WithQueueStateMetrics           = persistence.WithQueueStateMetrics
)
