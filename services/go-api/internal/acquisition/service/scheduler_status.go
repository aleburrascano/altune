package service

import (
	"altune/go-api/internal/acquisition/ports"
	"log/slog"
	"time"
)

func (s *BackgroundAcquisitionScheduler) workerCount() int {
	return max(cap(s.sem), 1)
}

func (s *BackgroundAcquisitionScheduler) Status() ports.AcquisitionStatus {
	jobs, recent := s.log.snapshot()
	succeeded, failed := s.log.counts()
	pending, oldestAge, depthKnown := s.pendingDepth()
	return ports.AcquisitionStatus{
		InFlight:         int(s.inflightCount.Load()),
		Succeeded:        succeeded,
		Failed:           failed,
		Rejected:         s.rejected.Load(),
		VerifySkipped:    s.verifySkipped(),
		Paused:           s.paused.Load(),
		QueueDepth:       pending,
		OldestPendingAge: oldestAge,
		QueueDepthKnown:  depthKnown,
		Workers:          s.workerCount(),
		Verification:     s.verificationStatus(),
		ActiveJobs:       jobs,
		Recent:           recent,
	}
}

func (s *BackgroundAcquisitionScheduler) pendingDepth() (int, time.Duration, bool) {
	reader, ok := s.queue.(ports.QueueDepthReader)
	if !ok {
		return 0, 0, false
	}
	pending, oldestAge, err := reader.PendingDepth(s.baseCtx)
	if err != nil {
		slog.ErrorContext(s.baseCtx, "acquisition.pending_depth_failed", "error", err)
		return 0, 0, false
	}
	return pending, oldestAge, true
}
