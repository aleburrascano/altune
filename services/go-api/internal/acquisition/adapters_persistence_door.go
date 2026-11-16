package acquisition

import persistence "altune/go-api/internal/acquisition/internal/adapters/persistence"

var (
	NewPgxCooldownStore  = persistence.NewPgxCooldownStore
	NewPgxJobNotifier    = persistence.NewPgxJobNotifier
	NewPgxJobQueue       = persistence.NewPgxJobQueue
	NewPgxOutcomeStore   = persistence.NewPgxOutcomeStore
	NewPgxRejectionStore = persistence.NewPgxRejectionStore
	NewPgxUpgradeStore   = persistence.NewPgxUpgradeStore
)
