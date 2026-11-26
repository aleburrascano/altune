package searchtest

import (
	"altune/go-api/internal/search/internal/adapters/memory"
	"altune/go-api/internal/search/internal/core/favorites"
	"altune/go-api/internal/search/internal/core/ports"
	"testing"
)

type FakeFavoritesRepo = memory.FakeFavoritesRepo

var (
	Subtitles  = memory.Subtitles
	FavoriteOf = memory.FavoriteOf
)

func NewFavorites(t testing.TB, repo ports.FavoritesRepository) *favorites.FavoritesService {
	t.Helper()
	svc, err := favorites.NewFavoritesService(repo)
	if err != nil {
		t.Fatalf("favorites.NewFavoritesService: %v", err)
	}
	return svc
}
