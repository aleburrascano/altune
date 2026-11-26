package service

import (
	acqdomain "altune/go-api/internal/download/internal/core/domain"
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/shared/background"
	"context"
	"errors"
	"log/slog"
	"time"
)

func (s *BackgroundAcquisitionScheduler) heartbeatLoop(ctx context.Context, job ports.Job, cancelJob context.CancelFunc, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(s.heartbeatInterval)
	defer ticker.Stop()
	lastSuccess := s.now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !s.sendHeartbeat(ctx, job, &lastSuccess) {
				cancelJob()
				return
			}
		}
	}
}

func (s *BackgroundAcquisitionScheduler) sendHeartbeat(jobCtx context.Context, job ports.Job, lastSuccess *time.Time) bool {
	ctx, cancel := background.Detach(jobCtx, queueCallBudget)
	defer cancel()
	err := s.queue.Heartbeat(ctx, job.TrackID, job.ClaimNumber, s.leaseDuration)
	if err == nil {
		*lastSuccess = s.now()
		return true
	}
	if errors.Is(err, acqdomain.ErrLeaseLost) {
		slog.WarnContext(ctx, "acquisition.lease_lost", "track_id", job.TrackID.String())
		return false
	}
	if s.now().Sub(*lastSuccess) >= s.leaseDuration {
		slog.ErrorContext(ctx, "acquisition.heartbeat_lease_expired", "track_id", job.TrackID.String(), "error", err)
		return false
	}
	slog.ErrorContext(ctx, "acquisition.heartbeat_failed", "track_id", job.TrackID.String(), "error", err)
	return true
}
