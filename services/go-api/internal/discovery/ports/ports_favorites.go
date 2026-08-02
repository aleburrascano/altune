package ports

import (
	"context"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared"
)

type FavoritesRepository interface {
	Add(ctx context.Context, userId shared.UserId, fav domain.Favorite) error
	Remove(ctx context.Context, userId shared.UserId, kind domain.ResultKind, key string) error
	ListForUser(ctx context.Context, userId shared.UserId) ([]domain.Favorite, error)
}
