package search

import favorites "altune/go-api/internal/search/internal/core/favorites"

var (
	NewFavoritesService              = favorites.NewFavoritesService
	NewFavoritesLifter               = favorites.NewFavoritesLifter
	ErrFavoritesCollaboratorRequired = favorites.ErrCollaboratorRequired
)
