package service

import (
	"altune/go-api/internal/acquisition/ports"
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
			if !s.sendHeartbeat(job, &lastSuccess) {
				cancelJob()
				return
			}
		}
	}
}

func (s *BackgroundAcquisitionScheduler) sendHeartbeat(job ports.Job, lastSuccess *time.Time) bool {
	err := s.queue.Heartbeat(context.Background(), job.TrackID, job.Fence, s.leaseDuration)
	if err == nil {
		*lastSuccess = s.now()
		return true
	}
	if errors.Is(err, ports.ErrLeaseLost) {
		slog.Warn("acquisition.lease_lost", "track_id", job.TrackID.String())
		return false
	}
	if s.now().Sub(*lastSuccess) >= s.leaseDuration {
		slog.Error("acquisition.heartbeat_lease_expired", "track_id", job.TrackID.String(), "error", err)
		return false
	}
	slog.Error("acquisition.heartbeat_failed", "track_id", job.TrackID.String(), "error", err)
	return true
}
