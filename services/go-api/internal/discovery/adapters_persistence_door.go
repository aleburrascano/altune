package discovery

import persistence "altune/go-api/internal/discovery/internal/adapters/persistence"

var (
	NewPgxEventStore              = persistence.NewPgxEventStore
	NewPgxFavoritesRepository     = persistence.NewPgxFavoritesRepository
	NewPgxSearchHistoryRepository = persistence.NewPgxSearchHistoryRepository
)

const AggregateEventRetention = persistence.AggregateEventRetention
