package memory

import (
	"altune/go-api/internal/search/internal/ports"
	"altune/go-api/internal/search/internal/service"
)

func NewFavorites(t TB, repo ports.FavoritesRepository) *service.FavoritesService {
	t.Helper()
	svc, err := service.NewFavoritesService(repo)
	if err != nil {
		t.Fatalf("favorites.NewFavoritesService: %v", err)
	}
	return svc
}
