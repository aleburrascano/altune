package searchtest

import (
	"altune/go-api/internal/search/internal/adapters/memory"
	"altune/go-api/internal/search/internal/ports"
	"altune/go-api/internal/search/internal/service"
	"testing"
)

type FakeFavoritesRepo = memory.FakeFavoritesRepo

var (
	Subtitles  = memory.Subtitles
	FavoriteOf = memory.FavoriteOf
)

func NewFavorites(t testing.TB, repo ports.FavoritesRepository) *service.FavoritesService {
	t.Helper()
	svc, err := service.NewFavoritesService(repo)
	if err != nil {
		t.Fatalf("favorites.NewFavoritesService: %v", err)
	}
	return svc
}
