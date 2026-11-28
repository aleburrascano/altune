package ports

import (
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/shared/ids"
	"context"
	"errors"
)

const MaxFavoritesPerUser = 1000

var ErrFavoritesFull = errors.New("favorites are full")

type FavoritesRepository interface {
	Add(ctx context.Context, userId ids.UserId, fav domain.Favorite) error
	Remove(ctx context.Context, userId ids.UserId, kind domain.ResultKind, key string) error
	ListForUser(ctx context.Context, userId ids.UserId) ([]domain.Favorite, error)
}
