package service

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/shared/logging"
	"context"
	"log/slog"
	"time"
)

func (s *BackgroundAcquisitionScheduler) workerCount() int {
	return s.workers
}

func (s *BackgroundAcquisitionScheduler) Status(ctx context.Context) domain.SchedulerStatus {
	jobs, recent := s.log.snapshot()
	succeeded, failed := s.log.counts()
	pending, oldestAge, depthKnown := s.pendingDepth(ctx)
	return domain.SchedulerStatus{
		InFlight:         s.runner.inFlight(),
		Pending:          s.pending.size(),
		Succeeded:        succeeded,
		Failed:           failed,
		Rejected:         s.rejected.Load(),
		VerifySkipped:    s.verifySkipped(),
		Paused:           s.gate.paused.Load(),
		QueueDepth:       pending,
		OldestPendingAge: oldestAge,
		QueueDepthKnown:  depthKnown,
		Workers:          s.workerCount(),
		Verification:     s.verificationStatus(),
		ActiveJobs:       jobs,
		Recent:           recent,
	}
}

func (s *BackgroundAcquisitionScheduler) pendingDepth(ctx context.Context) (int, time.Duration, bool) {
	pending, oldestAge, err := s.queue.PendingDepth(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "acquisition.pending_depth_failed", logging.KeyError, err)
		return 0, 0, false
	}
	return pending, oldestAge, true
}
