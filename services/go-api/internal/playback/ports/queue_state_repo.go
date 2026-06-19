package ports

import (
	"context"

	"altune/go-api/internal/playback/domain"
	"altune/go-api/internal/shared"
)

type QueueStateRepository interface {
	Upsert(ctx context.Context, state *domain.QueueState) error
	GetForUser(ctx context.Context, userId shared.UserId) (*domain.QueueState, error)
}
