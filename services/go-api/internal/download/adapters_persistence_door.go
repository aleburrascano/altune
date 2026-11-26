package download

import persistence "altune/go-api/internal/download/internal/adapters/persistence"

var (
	NewPostgresCooldownStore  = persistence.NewPostgresCooldownStore
	NewPostgresJobNotifier    = persistence.NewPostgresJobNotifier
	NewPostgresJobQueue       = persistence.NewPostgresJobQueue
	NewPostgresOutcomeStore   = persistence.NewPostgresOutcomeStore
	NewPostgresRejectionStore = persistence.NewPostgresRejectionStore
	NewPostgresUpgradeStore   = persistence.NewPostgresUpgradeStore
)
