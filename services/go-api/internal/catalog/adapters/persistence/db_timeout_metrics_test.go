package persistence

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

type countingDBCallMetrics struct{ timeouts atomic.Int64 }

func (m *countingDBCallMetrics) DBCallTimedOut() { m.timeouts.Add(1) }

// TestWithDBTimeout_CountsOnlyItsOwnDeadline proves the DB-call timeout counter
// moves when the persistence per-call deadline cuts off a wedged call, and stays
// put when the caller gave up first or the call completed in time.
func TestWithDBTimeout_CountsOnlyItsOwnDeadline(t *testing.T) {
	restore := dbCallTimeout
	dbCallTimeout = 50 * time.Millisecond
	metrics := &countingDBCallMetrics{}
	SetDBCallMetrics(metrics)
	t.Cleanup(func() {
		dbCallTimeout = restore
		SetDBCallMetrics(nil)
	})

	repo := &PgxTrackRepository{pool: blockingPool{}}
	userID := shared.NewUserId(uuid.New())

	t.Run("wedged call cut off by the per-call deadline is counted", func(t *testing.T) {
		before := metrics.timeouts.Load()
		if _, err := repo.GetByID(context.Background(), domain.NewTrackId(), userID); err == nil {
			t.Fatal("want a deadline error from the wedged pool, got nil")
		}
		if got := metrics.timeouts.Load(); got != before+1 {
			t.Errorf("DB-call timeouts = %d, want %d", got, before+1)
		}
	})

	t.Run("caller deadline is not a DB-call timeout", func(t *testing.T) {
		before := metrics.timeouts.Load()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
		defer cancel()
		if _, err := repo.GetByID(ctx, domain.NewTrackId(), userID); err == nil {
			t.Fatal("want a context error, got nil")
		}
		if got := metrics.timeouts.Load(); got != before {
			t.Errorf("DB-call timeouts = %d, want %d (caller deadline must not count)", got, before)
		}
	})

	t.Run("call finished within the deadline is not counted", func(t *testing.T) {
		before := metrics.timeouts.Load()
		_, cancel := withDBTimeout(context.Background())
		cancel()
		if got := metrics.timeouts.Load(); got != before {
			t.Errorf("DB-call timeouts = %d, want %d", got, before)
		}
	})

	t.Run("repeated cancel after a timeout counts once", func(t *testing.T) {
		before := metrics.timeouts.Load()
		ctx, cancel := withDBTimeout(context.Background())
		<-ctx.Done()
		cancel()
		cancel()
		if got := metrics.timeouts.Load(); got != before+1 {
			t.Errorf("DB-call timeouts = %d, want %d", got, before+1)
		}
	})
}
