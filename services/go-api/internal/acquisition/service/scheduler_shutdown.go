package service

import (
	"context"
	"log/slog"
	"time"
)

func (s *BackgroundAcquisitionScheduler) closeAdmission() {
	s.admitMu.Lock()
	defer s.admitMu.Unlock()
	s.closed.Store(true)
	s.closeOnce.Do(func() { close(s.stop) })
}

func (s *BackgroundAcquisitionScheduler) Shutdown(ctx context.Context) {
	s.closeAdmission()
	defer s.pending.sweep()

	done := make(chan struct{})
	go func() {
		s.jobsWG.Wait()
		close(done)
	}()

	drained := s.awaitJobsWG(done, s.drainBudget, ctx)
	if drained {
		slog.Info("acquisition scheduler drained")
	} else {
		slog.Warn("acquisition drain budget exceeded, cancelling in-flight jobs for release", "budget", s.drainBudget.String())
	}

	s.cancel()
	if !drained {
		drained = s.awaitJobsWG(done, 0, ctx)
		if !drained {
			slog.Warn("acquisition scheduler drain timed out waiting for cancelled jobs to release")
		}
	}

	s.waitRunners(ctx)
	if !drained {
		slog.Warn("acquisition scheduler shutdown ended with jobs still in flight, skipping the outcome wait")
		return
	}
	s.waitOutcomes(ctx)
}

func (s *BackgroundAcquisitionScheduler) awaitJobsWG(done <-chan struct{}, budget time.Duration, ctx context.Context) bool {
	var timerC <-chan time.Time
	if budget > 0 {
		timer := time.NewTimer(budget)
		defer timer.Stop()
		timerC = timer.C
	}
	select {
	case <-done:
		return true
	case <-timerC:
		return false
	case <-ctx.Done():
		return false
	}
}

func (s *BackgroundAcquisitionScheduler) waitRunners(ctx context.Context) {
	done := make(chan struct{})
	go func() {
		s.runnersWG.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		slog.Warn("acquisition scheduler shutdown context ended before every worker goroutine exited")
	}
}

func (s *BackgroundAcquisitionScheduler) waitOutcomes(ctx context.Context) {
	done := make(chan struct{})
	go func() {
		s.outcomeWG.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		slog.Warn("acquisition scheduler shutdown context ended before every outcome was recorded")
	}
}
