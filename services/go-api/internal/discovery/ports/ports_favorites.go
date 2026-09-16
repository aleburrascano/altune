package ports

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared"
	"context"
)

type FavoritesRepository interface {
	Add(ctx context.Context, userId shared.UserId, fav domain.Favorite) error
	Remove(ctx context.Context, userId shared.UserId, kind domain.ResultKind, key string) error
	ListForUser(ctx context.Context, userId shared.UserId) ([]domain.Favorite, error)
}
