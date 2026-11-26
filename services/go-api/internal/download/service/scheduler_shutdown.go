package service

import (
	"altune/go-api/internal/shared/background"
	"context"
	"log/slog"
)

func (s *BackgroundAcquisitionScheduler) closeAdmission() {
	s.admitMu.Lock()
	defer s.admitMu.Unlock()
	s.closed.Store(true)
	s.closeOnce.Do(func() { close(s.stop) })
}

func (s *BackgroundAcquisitionScheduler) Shutdown(ctx context.Context) {
	s.closeAdmission()

	drained := s.drainWithinBudget(ctx)

	s.cancel()
	if !drained {
		drained = s.drainCancelledJobs(ctx)
	}

	s.waitRunners(ctx)
	if !drained {
		slog.WarnContext(ctx, "acquisition scheduler shutdown ended with jobs still in flight, skipping the outcome wait")
		return
	}
	s.waitOutcomes(ctx)
}

func (s *BackgroundAcquisitionScheduler) drainCancelledJobs(ctx context.Context) bool {
	if s.jobs.Drain(ctx) {
		return true
	}
	slog.WarnContext(ctx, "acquisition scheduler drain timed out waiting for cancelled jobs to release")
	return false
}

func (s *BackgroundAcquisitionScheduler) drainWithinBudget(ctx context.Context) bool {
	budget, cancel := context.WithTimeout(ctx, s.drainBudget)
	defer cancel()
	if s.jobs.Drain(budget) {
		slog.InfoContext(ctx, "acquisition scheduler drained")
		return true
	}
	slog.WarnContext(ctx, "acquisition drain budget exceeded, cancelling in-flight jobs for release", "budget", s.drainBudget.String())
	return false
}

func (s *BackgroundAcquisitionScheduler) waitRunners(ctx context.Context) {
	if !background.WaitWithin(ctx, s.runners.Wait) {
		slog.WarnContext(ctx, "acquisition scheduler shutdown context ended before every worker goroutine exited")
	}
}

func (s *BackgroundAcquisitionScheduler) waitOutcomes(ctx context.Context) {
	if !s.outcomeRecords.Drain(ctx) {
		slog.WarnContext(ctx, "acquisition scheduler shutdown context ended before every outcome was recorded")
	}
}
