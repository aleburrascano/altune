package service

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"context"
	"fmt"
	"log/slog"
	"time"
)

const DefaultStalePendingGrace = 15 * time.Minute

type ReconcileStalePendingService struct {
	repo  ports.StalePendingFailer
	grace time.Duration
	now   func() time.Time
}

func NewReconcileStalePendingService(repo ports.StalePendingFailer, opts ...func(*ReconcileStalePendingService)) *ReconcileStalePendingService {
	s := &ReconcileStalePendingService{repo: repo, grace: DefaultStalePendingGrace, now: time.Now}
	return applyOptions(s, opts)
}

func WithStalePendingGrace(grace time.Duration) func(*ReconcileStalePendingService) {
	return func(s *ReconcileStalePendingService) {
		if grace > 0 {
			s.grace = grace
		}
	}
}

func WithStalePendingClock(now func() time.Time) func(*ReconcileStalePendingService) {
	return func(s *ReconcileStalePendingService) {
		if now != nil {
			s.now = now
		}
	}
}

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
