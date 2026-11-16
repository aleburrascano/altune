package discovery

import persistence "altune/go-api/internal/discovery/internal/adapters/persistence"

type (
	PgxEventStore      = persistence.PgxEventStore
	PgxUnreleasedStore = persistence.PgxUnreleasedStore
)

var (
	NewPgxEventStore              = persistence.NewPgxEventStore
	NewPgxFavoritesRepository     = persistence.NewPgxFavoritesRepository
	NewPgxIdentityStore           = persistence.NewPgxIdentityStore
	NewPgxRelationshipQuerier     = persistence.NewPgxRelationshipQuerier
	NewPgxSearchHistoryRepository = persistence.NewPgxSearchHistoryRepository
	NewPgxUnreleasedStore         = persistence.NewPgxUnreleasedStore
)

const AggregateEventRetention = persistence.AggregateEventRetention
