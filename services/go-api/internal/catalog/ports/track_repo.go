package ports

import (
	"context"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
)

type TrackRepository interface {
	// Add inserts the track, or returns the existing one on a dedup-key conflict.
	// The caller gets the persisted/existing track back (created reports which) —
	// no second lookup needed at the call site.
	Add(ctx context.Context, track *domain.Track) (stored *domain.Track, created bool, err error)
	GetByID(ctx context.Context, id domain.TrackId, userId shared.UserId) (*domain.Track, error)
	GetByDedupKey(ctx context.Context, userId shared.UserId, dedupKey string) (*domain.Track, error)
	ListForUser(ctx context.Context, userId shared.UserId, limit, offset int) (tracks []*domain.Track, total int, err error)
	Update(ctx context.Context, track *domain.Track) error
	Delete(ctx context.Context, id domain.TrackId, userId shared.UserId) (deleted bool, err error)
}
