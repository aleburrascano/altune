package service

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
	"context"
	"errors"
	"log/slog"
)

func (s *BackgroundAcquisitionScheduler) ScheduleReplace(ctx context.Context, userId ids.UserId, trackId ids.TrackId) error {
	return s.enqueue(ctx, userId, trackId, domain.JobKindReplace)
}

func (s *BackgroundAcquisitionScheduler) ScheduleUpgrade(ctx context.Context, userId ids.UserId, trackId ids.TrackId) error {
	return s.enqueue(ctx, userId, trackId, domain.JobKindUpgrade)
}

func (s *BackgroundAcquisitionScheduler) Schedule(ctx context.Context, userId ids.UserId, trackId ids.TrackId, _ string) error {
	return s.enqueue(ctx, userId, trackId, domain.JobKindAcquire)
}

func (s *BackgroundAcquisitionScheduler) enqueue(ctx context.Context, userId ids.UserId, trackId ids.TrackId, kind domain.JobKind) error {
	ctx = logging.ContextWithSubjectUser(ctx, userId.String())
	if s.gate.closed.Load() {
		s.rejected.Add(1)
		slog.WarnContext(ctx, "schedule_after_shutdown", logging.KeyTrackID, trackId.String())
		return domain.ErrSchedulerShutdown
	}
	if s.gate.paused.Load() {
		s.rejected.Add(1)
		slog.WarnContext(ctx, "acquisition.schedule_while_paused", logging.KeyTrackID, trackId.String())
		return domain.ErrAcquisitionPaused
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

func (s *BackgroundAcquisitionScheduler) finishEnqueue(ctx context.Context, trackId ids.TrackId, userId ids.UserId, kind domain.JobKind) error {
	if s.gate.closed.Load() {
		s.pending.untrack(trackId.String())
	}
	slog.InfoContext(ctx, "acquisition.scheduling", logging.KeyTrackID, trackId.String(), "kind", string(kind))
	return nil
}

func (s *BackgroundAcquisitionScheduler) enqueueFailed(ctx context.Context, trackId ids.TrackId, kind domain.JobKind, err error) error {
	if errors.Is(err, domain.ErrJobKindConflict) {
		s.rejected.Add(1)
		slog.WarnContext(ctx, "acquisition.job_in_flight",
			logging.KeyTrackID, trackId.String(), "requested_kind", string(kind))
		return domain.ErrTrackJobInFlight
	}
	return err
}
