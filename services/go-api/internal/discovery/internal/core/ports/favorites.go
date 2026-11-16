package ports

import (
	"altune/go-api/internal/discovery/domain"
	shared "altune/go-api/internal/shared/kernel"
	"context"
	"errors"
)

const MaxFavoritesPerUser = 1000

var ErrFavoritesFull = errors.New("favorites are full")

type FavoritesRepository interface {
	Add(ctx context.Context, userId shared.UserId, fav domain.Favorite) error
	Remove(ctx context.Context, userId shared.UserId, kind domain.ResultKind, key string) error
	ListForUser(ctx context.Context, userId shared.UserId) ([]domain.Favorite, error)
}
