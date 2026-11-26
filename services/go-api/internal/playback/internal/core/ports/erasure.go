package ports

import (
	"altune/go-api/internal/shared/ids"
	"context"
	"errors"
)

var ErrIdentityStoreUnavailable = errors.New("identity store unavailable")

type OwnerScanPage struct {
	Orphaned []ids.UserId
	Next     ids.UserId
	Done     bool
}

type DeletedUserDataLister interface {
	ListOwnersWithoutIdentity(ctx context.Context, after ids.UserId, window int) (OwnerScanPage, error)
}

type DeletedUserDataCleaner interface {
	ReapErasedStates(ctx context.Context) (int64, error)
}

type ErasureSweepMetrics interface {
	SweepIdle()
	QueueStateErased(n int)
	OwnerScanTimedOut()
	RetryOwnerDropped()
}

func NoopErasureSweepMetrics() ErasureSweepMetrics { return noopErasureSweepMetrics{} }

type noopErasureSweepMetrics struct{}

func (noopErasureSweepMetrics) SweepIdle()           {}
func (noopErasureSweepMetrics) QueueStateErased(int) {}
func (noopErasureSweepMetrics) OwnerScanTimedOut()   {}
func (noopErasureSweepMetrics) RetryOwnerDropped()   {}
