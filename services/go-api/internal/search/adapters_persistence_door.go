package search

import persistence "altune/go-api/internal/search/internal/adapters/persistence"

var (
	NewPostgresEventStore              = persistence.NewPostgresEventStore
	NewPostgresFavoritesRepository     = persistence.NewPostgresFavoritesRepository
	NewPostgresSearchHistoryRepository = persistence.NewPostgresSearchHistoryRepository
)
