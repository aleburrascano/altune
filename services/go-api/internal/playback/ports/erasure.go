package ports

import (
	"altune/go-api/internal/shared"
	"context"
	"errors"
)

var ErrIdentityStoreUnavailable = errors.New("identity store unavailable")

type OwnerScanPage struct {
	Orphaned []shared.UserId
	Next     shared.UserId
	Done     bool
}

type DeletedIdentityLister interface {
	ListOwnersWithoutIdentity(ctx context.Context, after shared.UserId, window int) (OwnerScanPage, error)
}

type ErasedStateReaper interface {
	ReapErasedStates(ctx context.Context) (int64, error)
}

type ErasureSweepMetrics interface {
	SweepIdle()
	QueueStateErased(n int)
	OwnerScanTimedOut()
}

func NoopErasureSweepMetrics() ErasureSweepMetrics { return noopErasureSweepMetrics{} }

type noopErasureSweepMetrics struct{}

func (noopErasureSweepMetrics) SweepIdle()           {}
func (noopErasureSweepMetrics) QueueStateErased(int) {}
func (noopErasureSweepMetrics) OwnerScanTimedOut()   {}
