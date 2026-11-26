package download

import persistence "altune/go-api/internal/download/internal/adapters/persistence"

type PostgresCooldownStore = persistence.PostgresCooldownStore

var (
	ErrNotifierSleeperRequired = persistence.ErrNotifierSleeperRequired
	NewPostgresCooldownStore   = persistence.NewPostgresCooldownStore
	NewPostgresJobNotifier     = persistence.NewPostgresJobNotifier
	MustNewPostgresJobNotifier = persistence.MustNewPostgresJobNotifier
	NewPostgresJobQueue        = persistence.NewPostgresJobQueue
	NewPostgresOutcomeStore    = persistence.NewPostgresOutcomeStore
	NewPostgresRejectionStore  = persistence.NewPostgresRejectionStore
	NewPostgresUpgradeStore    = persistence.NewPostgresUpgradeStore
)
