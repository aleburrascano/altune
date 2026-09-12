package ports

import (
	"context"

	"altune/go-api/internal/playback/domain"
	"altune/go-api/internal/shared"
)

type QueueStateRepository interface {
	Upsert(ctx context.Context, state *domain.QueueState) error
	GetForUser(ctx context.Context, userId shared.UserId) (*domain.QueueState, error)
	// DeleteForUser erases the user's persisted queue state (PII: track list,
	// natural order, and free-text search source_id). Idempotent: deleting a
	// user with no stored state is not an error.
	DeleteForUser(ctx context.Context, userId shared.UserId) error
}
