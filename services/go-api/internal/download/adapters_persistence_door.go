package download

import persistence "altune/go-api/internal/download/internal/adapters/persistence"

type PostgresJobQueue = persistence.PostgresJobQueue

var (
	NewPostgresCooldownStore   = persistence.NewPostgresCooldownStore
	NewPostgresDatabase        = persistence.NewDatabase
	MustNewPostgresJobNotifier = persistence.MustNewPostgresJobNotifier
	MustNewPostgresJobQueue    = persistence.MustNewPostgresJobQueue
	NewPostgresOutcomeStore    = persistence.NewPostgresOutcomeStore
	NewPostgresRejectionStore  = persistence.NewPostgresRejectionStore
	NewPostgresUpgradeStore    = persistence.NewPostgresUpgradeStore
)
