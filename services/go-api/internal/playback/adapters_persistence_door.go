package playback

import persistence "altune/go-api/internal/playback/internal/adapters/persistence"

var (
	NewPostgresDeletedIdentityRepository = persistence.NewPostgresDeletedIdentityRepository
	NewPostgresQueueStateRepository      = persistence.NewPostgresQueueStateRepository
)
