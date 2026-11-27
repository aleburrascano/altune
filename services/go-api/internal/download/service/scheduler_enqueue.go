package service

import (
	acqdomain "altune/go-api/internal/download/internal/core/domain"
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
	"context"
	"errors"
	"log/slog"
)

func (s *BackgroundAcquisitionScheduler) ScheduleReplace(ctx context.Context, userId ids.UserId, trackId ids.TrackId) error {
	return s.enqueue(ctx, userId, trackId, ports.JobKindReplace)
}

func (s *BackgroundAcquisitionScheduler) ScheduleUpgrade(ctx context.Context, userId ids.UserId, trackId ids.TrackId) error {
	return s.enqueue(ctx, userId, trackId, ports.JobKindUpgrade)
}

func (s *BackgroundAcquisitionScheduler) Schedule(ctx context.Context, userId ids.UserId, trackId ids.TrackId, _ string) error {
	return s.enqueue(ctx, userId, trackId, ports.JobKindAcquire)
}

func (s *BackgroundAcquisitionScheduler) enqueue(ctx context.Context, userId ids.UserId, trackId ids.TrackId, kind ports.JobKind) error {
	if s.closed.Load() {
		s.rejected.Add(1)
		slog.WarnContext(ctx, "schedule_after_shutdown", logging.KeyTrackID, trackId.String())
		return acqdomain.ErrSchedulerShutdown
	}
	if s.paused.Load() {
		s.rejected.Add(1)
		slog.WarnContext(ctx, "acquisition.schedule_while_paused", logging.KeyTrackID, trackId.String())
		return acqdomain.ErrAcquisitionPaused
	}

	key := trackId.String()
	alreadyTracked := s.pending.track(key)
	s.primeEnqueue(trackId, userId)

	if err := s.queue.Enqueue(ctx, trackId, kind, 0); err != nil {
		return s.handleEnqueueError(ctx, key, trackId, kind, alreadyTracked, err)
	}
	return s.finishEnqueue(ctx, trackId, userId, kind)
}

type userRememberer interface {
	RememberUser(trackId ids.TrackId, userId ids.UserId)
}

func (s *BackgroundAcquisitionScheduler) primeEnqueue(trackId ids.TrackId, userId ids.UserId) {
	if r, ok := s.queue.(userRememberer); ok {
		r.RememberUser(trackId, userId)
	}
}

func (s *BackgroundAcquisitionScheduler) finishEnqueue(ctx context.Context, trackId ids.TrackId, userId ids.UserId, kind ports.JobKind) error {
	if s.closed.Load() {
		s.pending.untrack(trackId.String())
	}
	slog.InfoContext(ctx, "acquisition.scheduling", logging.KeyTrackID, trackId.String(), logging.KeyUserID, userId.String(), "kind", string(kind))
	return nil
}

func (s *BackgroundAcquisitionScheduler) handleEnqueueError(ctx context.Context, key string, trackId ids.TrackId, kind ports.JobKind, alreadyTracked bool, err error) error {
	if !alreadyTracked {
		s.pending.untrack(key)
	}
	if errors.Is(err, acqdomain.ErrJobKindConflict) {
		s.rejected.Add(1)
		slog.WarnContext(ctx, "acquisition.job_in_flight",
			logging.KeyTrackID, trackId.String(), "requested_kind", string(kind))
		return acqdomain.ErrTrackJobInFlight
	}
	return err
}
