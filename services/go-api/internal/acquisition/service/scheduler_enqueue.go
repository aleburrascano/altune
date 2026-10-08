package service

import (
	acqdomain "altune/go-api/internal/acquisition/domain"
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/logging"
	"context"
	"errors"
	"log/slog"
)

func (s *BackgroundAcquisitionScheduler) ScheduleReplace(ctx context.Context, userId shared.UserId, trackId domain.TrackId) error {
	return s.enqueue(ctx, userId, trackId, ports.JobKindReplace)
}

func (s *BackgroundAcquisitionScheduler) Schedule(ctx context.Context, userId shared.UserId, trackId domain.TrackId, _ string) error {
	return s.enqueue(ctx, userId, trackId, ports.JobKindAcquire)
}

func (s *BackgroundAcquisitionScheduler) enqueue(ctx context.Context, userId shared.UserId, trackId domain.TrackId, kind ports.JobKind) error {
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
	s.primeEnqueue(ctx, trackId, userId, key, alreadyTracked)

	if err := s.queue.Enqueue(ctx, trackId, kind, 0); err != nil {
		return s.handleEnqueueError(ctx, key, trackId, kind, alreadyTracked, err)
	}
	return s.finishEnqueue(ctx, trackId, userId, kind)
}

type userRememberer interface {
	rememberUser(trackId domain.TrackId, userId shared.UserId)
}

func (s *BackgroundAcquisitionScheduler) primeEnqueue(ctx context.Context, trackId domain.TrackId, userId shared.UserId, key string, alreadyTracked bool) {
	if r, ok := s.queue.(userRememberer); ok {
		r.rememberUser(trackId, userId)
	}
	if corrID := logging.CorrelationIDFromContext(ctx); corrID != "" && !alreadyTracked {
		s.corrIDs.Store(key, corrID)
	}
}

func (s *BackgroundAcquisitionScheduler) finishEnqueue(ctx context.Context, trackId domain.TrackId, userId shared.UserId, kind ports.JobKind) error {
	if s.closed.Load() {
		s.pending.untrack(trackId.String())
	}
	slog.InfoContext(ctx, "acquisition.scheduling", "track_id", trackId.String(), "user_id", userId.String(), "kind", string(kind))
	return nil
}

func (s *BackgroundAcquisitionScheduler) handleEnqueueError(ctx context.Context, key string, trackId domain.TrackId, kind ports.JobKind, alreadyTracked bool, err error) error {
	if !alreadyTracked {
		s.pending.untrack(key)
		s.corrIDs.Delete(key)
	}
	if errors.Is(err, ports.ErrJobKindConflict) {
		s.rejected.Add(1)
		slog.WarnContext(ctx, "acquisition.job_in_flight",
			"track_id", trackId.String(), "requested_kind", string(kind))
		return acqdomain.ErrTrackJobInFlight
	}
	return err
}
