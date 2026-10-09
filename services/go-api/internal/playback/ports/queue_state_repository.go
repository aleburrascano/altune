package ports

import (
	"altune/go-api/internal/playback/domain"
	"altune/go-api/internal/shared"
	"context"
	"errors"
)

var ErrCorruptStoredState = errors.New("corrupt stored queue state")

type QueueStateRepository interface {
	Upsert(ctx context.Context, state *domain.QueueState) error
	UpdatePosition(ctx context.Context, position *domain.QueuePosition) error
	GetForUser(ctx context.Context, userId shared.UserId) (*domain.QueueState, error)
	DeleteForUser(ctx context.Context, userId shared.UserId) error
}
