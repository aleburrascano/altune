package service

import (
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/shared/logging"
	"context"
	"log/slog"
	"time"
)

func (s *BackgroundAcquisitionScheduler) workerCount() int {
	return s.workers
}

func (s *BackgroundAcquisitionScheduler) Status(ctx context.Context) ports.Status {
	jobs, recent := s.log.snapshot()
	succeeded, failed := s.log.counts()
	pending, oldestAge, depthKnown := s.pendingDepth(ctx)
	return ports.Status{
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
	reader, ok := s.queue.(ports.QueueDepthReader)
	if !ok {
		return 0, 0, false
	}
	pending, oldestAge, err := reader.PendingDepth(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "acquisition.pending_depth_failed", logging.KeyError, err)
		return 0, 0, false
	}
	return pending, oldestAge, true
}
