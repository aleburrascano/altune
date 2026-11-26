package download

import persistence "altune/go-api/internal/download/internal/adapters/persistence"

var (
	NewPostgresCooldownStore   = persistence.NewPostgresCooldownStore
	MustNewPostgresJobNotifier = persistence.MustNewPostgresJobNotifier
	NewPostgresJobQueue        = persistence.NewPostgresJobQueue
	NewPostgresOutcomeStore    = persistence.NewPostgresOutcomeStore
	NewPostgresRejectionStore  = persistence.NewPostgresRejectionStore
	NewPostgresUpgradeStore    = persistence.NewPostgresUpgradeStore
)
