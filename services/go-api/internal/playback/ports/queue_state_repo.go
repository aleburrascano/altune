package ports

import (
	"altune/go-api/internal/playback/domain"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"time"
)

var ErrCorruptStoredState = errors.New("corrupt stored queue state")

type UnavailableError struct{}

func (*UnavailableError) Error() string     { return "queue state temporarily unavailable" }
func (*UnavailableError) HTTPStatus() int   { return 503 }
func (*UnavailableError) ErrorCode() string { return "playback.unavailable" }

func (*UnavailableError) RetryAfter() time.Duration { return time.Second }

var ErrQueueStateUnavailable error = &UnavailableError{}

type QueueStateRepository interface {
	Upsert(ctx context.Context, state *domain.QueueState) error
	UpdatePosition(ctx context.Context, position *domain.QueuePosition) error
	GetForUser(ctx context.Context, userId shared.UserId) (*domain.QueueState, error)
	DeleteForUser(ctx context.Context, userId shared.UserId) error
}
