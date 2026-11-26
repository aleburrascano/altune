package ports

import (
	"altune/go-api/internal/playback/internal/core/domain"
	"altune/go-api/internal/shared/ids"
	"context"
	"errors"
)

var ErrCorruptStoredState = errors.New("corrupt stored queue state")

var ErrQueueStateNotFound = errors.New("no stored queue state")

type QueueStateRepository interface {
	Upsert(ctx context.Context, state *domain.QueueState) error
	UpdatePosition(ctx context.Context, position *domain.QueuePosition) error
	GetForUser(ctx context.Context, userId ids.UserId) (*domain.QueueState, error)
	DeleteForUser(ctx context.Context, userId ids.UserId) error
}

type QueueStateMetrics interface {
	CorruptStoredState()
	QueueStateOpTimedOut()
	QueueStateOpFailedTransient()
	QueueStateStaleWrite()
}

func NoopQueueStateMetrics() QueueStateMetrics { return noopQueueStateMetrics{} }

type noopQueueStateMetrics struct{}

func (noopQueueStateMetrics) CorruptStoredState()          {}
func (noopQueueStateMetrics) QueueStateOpTimedOut()        {}
func (noopQueueStateMetrics) QueueStateOpFailedTransient() {}
func (noopQueueStateMetrics) QueueStateStaleWrite()        {}

type RateLimitMetrics interface {
	QueueStateRateLimited()
}
