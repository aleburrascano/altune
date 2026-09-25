package service

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"context"
	"fmt"
	"log/slog"
	"time"
)

// DefaultStalePendingGrace is how long a track may sit pending before its
// acquisition job is presumed lost to a dead process. It must comfortably exceed
// the longest legitimate acquisition so a live, in-progress job is never swept.
const DefaultStalePendingGrace = 15 * time.Minute

// ReconcileStalePendingService sweeps tracks left pending by an acquisition job
// that never completed (the process died mid-flight, so the in-memory goroutine
// was lost) and transitions them to failed, where the existing retry path can
// reclaim them. Run it on startup and on an interval.
type ReconcileStalePendingService struct {
	repo  ports.StalePendingFailer
	grace time.Duration
	now   func() time.Time
}

func NewReconcileStalePendingService(repo ports.StalePendingFailer, opts ...func(*ReconcileStalePendingService)) *ReconcileStalePendingService {
	s := &ReconcileStalePendingService{repo: repo, grace: DefaultStalePendingGrace, now: time.Now}
	return applyOptions(s, opts)
}

// WithStalePendingGrace overrides the staleness window. A non-positive value is
// ignored so the safe default always holds.
func WithStalePendingGrace(grace time.Duration) func(*ReconcileStalePendingService) {
	return func(s *ReconcileStalePendingService) {
		if grace > 0 {
			s.grace = grace
		}
	}
}

// WithStalePendingClock replaces the clock the cutoff is measured from. A nil
// clock is ignored so the wall clock always holds.
func WithStalePendingClock(now func() time.Time) func(*ReconcileStalePendingService) {
	return func(s *ReconcileStalePendingService) {
		if now != nil {
			s.now = now
		}
	}
}

// Execute fails every track that has been pending past the grace window and
// returns how many it recovered.
func (s *ReconcileStalePendingService) Execute(ctx context.Context) (int, error) {
	cutoff := s.now().UTC().Add(-s.grace)
	recovered, err := s.repo.FailStalePending(ctx, cutoff, string(domain.FailureAcquisitionInterrupted))
	if err != nil {
		return 0, fmt.Errorf("reconcile stale pending: %w", err)
	}
	if recovered > 0 {
		slog.InfoContext(ctx, "acquisition.stale_pending_reconciled",
			"recovered", recovered,
			"grace", s.grace.String(),
		)
	}
	return recovered, nil
}
