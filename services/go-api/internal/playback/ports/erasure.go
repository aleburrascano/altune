package ports

import (
	"altune/go-api/internal/shared"
	"context"
	"errors"
)

var ErrIdentityStoreUnavailable = errors.New("identity store unavailable")

type DeletedIdentityLister interface {
	ListOwnersWithoutIdentity(ctx context.Context, limit int) ([]shared.UserId, error)
}

type ErasedStateReaper interface {
	ReapErasedStates(ctx context.Context) (int64, error)
}

type ErasureSweepMetrics interface {
	SweepIdle()
	QueueStateErased(n int)
}

func NoopErasureSweepMetrics() ErasureSweepMetrics { return noopErasureSweepMetrics{} }

type noopErasureSweepMetrics struct{}

func (noopErasureSweepMetrics) SweepIdle()           {}
func (noopErasureSweepMetrics) QueueStateErased(int) {}
