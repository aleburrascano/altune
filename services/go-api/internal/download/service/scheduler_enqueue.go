package service

import (
	acqdomain "altune/go-api/internal/download/internal/core/domain"
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/shared/ids"
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
		slog.WarnContext(ctx, "schedule_after_shutdown", "track_id", trackId.String())
		return acqdomain.ErrSchedulerShutdown
	}
	if s.paused.Load() {
		s.rejected.Add(1)
		slog.WarnContext(ctx, "acquisition.schedule_while_paused", "track_id", trackId.String())
		return acqdomain.ErrAcquisitionPaused
	}

	key := trackId.String()
	alreadyTracked := s.pending.track(key)
	s.primeEnqueue(trackId, userId)

	if err := s.queue.Enqueue(ctx, trackId, kind, 0); err != nil {
		if !alreadyTracked {
			s.pending.untrack(key)
		}
		return s.enqueueFailed(ctx, trackId, kind, err)
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
	slog.InfoContext(ctx, "acquisition.scheduling", "track_id", trackId.String(), "user_id", userId.String(), "kind", string(kind))
	return nil
}

func (s *BackgroundAcquisitionScheduler) enqueueFailed(ctx context.Context, trackId ids.TrackId, kind ports.JobKind, err error) error {
	if errors.Is(err, acqdomain.ErrJobKindConflict) {
		s.rejected.Add(1)
		slog.WarnContext(ctx, "acquisition.job_in_flight",
			"track_id", trackId.String(), "requested_kind", string(kind))
		return acqdomain.ErrTrackJobInFlight
	}
	return err
}
