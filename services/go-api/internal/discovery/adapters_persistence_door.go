package discovery

import persistence "altune/go-api/internal/discovery/internal/adapters/persistence"

var (
	NewPgxEventStore              = persistence.NewPgxEventStore
	NewPgxFavoritesRepository     = persistence.NewPgxFavoritesRepository
	NewPgxIdentityStore           = persistence.NewPgxIdentityStore
	NewPgxRelationshipQuerier     = persistence.NewPgxRelationshipQuerier
	NewPgxSearchHistoryRepository = persistence.NewPgxSearchHistoryRepository
)

const AggregateEventRetention = persistence.AggregateEventRetention
