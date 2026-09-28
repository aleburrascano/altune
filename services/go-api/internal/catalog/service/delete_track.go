package service

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/events"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

type DeleteTrackService struct {
	trackRepo  ports.TrackAudioDeleter
	audioStore ports.AudioStore
	events     events.Publisher
	metrics    ports.AudioStoreMetrics
	orphans    ports.OrphanedAudioRecorder
}

const orphanRecordTimeout = 5 * time.Second

func NewDeleteTrackService(trackRepo ports.TrackAudioDeleter, audioStore ports.AudioStore, opts ...func(*DeleteTrackService)) *DeleteTrackService {
	s := &DeleteTrackService{trackRepo: trackRepo, audioStore: audioStore, events: events.NoopPublisher(), metrics: ports.NoopAudioStoreMetrics()}
	return applyOptions(s, opts)
}

func WithDeleteTrackEvents(pub events.Publisher) func(*DeleteTrackService) {
	return func(s *DeleteTrackService) {
		if pub != nil {
			s.events = pub
		}
	}
}

func WithDeleteTrackMetrics(m ports.AudioStoreMetrics) func(*DeleteTrackService) {
	return func(s *DeleteTrackService) {
		if m != nil {
			s.metrics = m
		}
	}
}

func WithDeleteTrackOrphanQueue(q ports.OrphanedAudioRecorder) func(*DeleteTrackService) {
	return func(s *DeleteTrackService) {
		if q != nil {
			s.orphans = q
		}
	}
}

func (s *DeleteTrackService) Execute(ctx context.Context, userId shared.UserId, trackId domain.TrackId) error {
	deleted, audioRef, err := s.trackRepo.Delete(ctx, trackId, userId)
	if err != nil {
		return fmt.Errorf("delete track: %w", err)
	}
	if !deleted {
		return ErrTrackNotFound
	}

	slog.InfoContext(ctx, "track deleted from library",
		"track_id", trackId.String(), "user_id", userId.String())
	s.events.Publish(ctx, userId, events.TypeTrackDeleted, map[string]any{
		"track_id": trackId.String(),
	})

	if audioRef == nil {
		return nil
	}
	return s.deleteAudio(ctx, userId, trackId, *audioRef)
}

func (s *DeleteTrackService) deleteAudio(ctx context.Context, userId shared.UserId, trackId domain.TrackId, audioRef string) error {
	inUse, err := s.trackRepo.AudioRefInUse(ctx, audioRef, trackId)
	if err != nil {
		return s.reportOrphanedAudio(ctx, userId, trackId, audioRef, fmt.Errorf("audio usage unknown: %w", err))
	}
	if inUse {
		slog.InfoContext(ctx, "audio kept: another track still references it",
			"event", "catalog.shared_audio_kept",
			"track_id", trackId.String(), "audio_ref", audioRef)
		return nil
	}
	if err := s.audioStore.Delete(ctx, audioRef); err != nil {
		return s.reportOrphanedAudio(ctx, userId, trackId, audioRef, err)
	}
	return nil
}

func (s *DeleteTrackService) reportOrphanedAudio(ctx context.Context, userId shared.UserId, trackId domain.TrackId, audioRef string, cause error) error {
	s.metrics.OrphanedDelete()
	queued := s.recordOrphan(ctx, userId, trackId, audioRef)
	slog.ErrorContext(ctx, "orphaned audio file after track delete",
		"event", "catalog.orphaned_audio",
		"track_id", trackId.String(),
		"user_id", userId.String(),
		"audio_ref", audioRef,
		"queued_for_retry", queued,
		"error", cause,
	)
	return fmt.Errorf("%w: %w", ErrAudioOrphaned, cause)
}

func (s *DeleteTrackService) recordOrphan(ctx context.Context, userId shared.UserId, trackId domain.TrackId, audioRef string) bool {
	if s.orphans == nil {
		return false
	}
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), orphanRecordTimeout)
	defer cancel()
	err := s.orphans.RecordOrphanedAudio(recordCtx, ports.OrphanedAudio{AudioRef: audioRef, UserId: userId, TrackId: trackId})
	if err == nil {
		return true
	}
	level := slog.LevelError
	if errors.Is(err, ports.ErrOrphanedAudioQueueUnavailable) {
		level = slog.LevelWarn
	}
	slog.Log(ctx, level, "orphaned audio not queued for retry", "audio_ref", audioRef, "error", err)
	return false
}
