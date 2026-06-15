package ports

import (
	"context"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
)

type TrackRepository interface {
	Add(ctx context.Context, track *domain.Track) (created bool, err error)
	GetByID(ctx context.Context, id domain.TrackId, userId shared.UserId) (*domain.Track, error)
	GetByDedupKey(ctx context.Context, userId shared.UserId, dedupKey string) (*domain.Track, error)
	ListForUser(ctx context.Context, userId shared.UserId, limit, offset int) (tracks []*domain.Track, total int, err error)
	Update(ctx context.Context, track *domain.Track) error
	Delete(ctx context.Context, id domain.TrackId, userId shared.UserId) (deleted bool, err error)
}
