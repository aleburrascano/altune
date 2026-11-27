package service

import (
	acqdomain "altune/go-api/internal/download/internal/core/domain"
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/logging"
	"context"
	"errors"
	"log/slog"
	"time"
)

func (s *BackgroundAcquisitionScheduler) heartbeatLoop(ctx context.Context, job ports.Job, cancelJob context.CancelFunc, done chan<- struct{}) {
	defer close(done)
	ticks, stop := s.ticker(s.heartbeatInterval)
	defer stop()
	lastSuccess := s.now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
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
		slog.WarnContext(ctx, "acquisition.lease_lost", logging.KeyTrackID, job.TrackID.String())
		return false
	}
	if s.now().Sub(*lastSuccess) >= s.leaseDuration {
		slog.ErrorContext(ctx, "acquisition.heartbeat_lease_expired", logging.KeyTrackID, job.TrackID.String(), logging.KeyError, err)
		return false
	}
	slog.ErrorContext(ctx, "acquisition.heartbeat_failed", logging.KeyTrackID, job.TrackID.String(), logging.KeyError, err)
	return true
}
