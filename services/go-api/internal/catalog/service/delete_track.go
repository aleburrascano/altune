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
	trackRepo  ports.TrackDeleter
	audioStore ports.AudioStore
	events     events.Publisher
	metrics    ports.AudioStoreMetrics
	orphans    ports.OrphanedAudioRecorder
}

// orphanRecordTimeout bounds the durable orphan write, which runs detached from
// the request context so a client disconnect cannot drop the record.
const orphanRecordTimeout = 5 * time.Second

func NewDeleteTrackService(trackRepo ports.TrackDeleter, audioStore ports.AudioStore, opts ...func(*DeleteTrackService)) *DeleteTrackService {
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

// WithDeleteTrackOrphanQueue durably records an audio object whose storage
// delete failed, so the orphaned audio reconcile job retries it. Without it
// (or before migration 021) an orphan is only logged and counted.
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

	// The row delete is the destructive, attributable action: log it before
	// the audio cleanup so the trail exists even when the delete is partial.
	slog.InfoContext(ctx, "track deleted from library",
		"track_id", trackId.String(), "user_id", userId.String())
	s.events.Publish(userId, events.TypeTrackDeleted, map[string]any{
		"track_id": trackId.String(),
	})

	if audioRef != nil {
		if err := s.audioStore.Delete(ctx, *audioRef); err != nil {
			s.metrics.OrphanedDelete()
			queued := s.recordOrphan(ctx, userId, trackId, *audioRef)
			// Marked log line so orphans are discoverable/reconcilable by querying
			// event=catalog.orphaned_audio rather than being lost in noise.
			slog.ErrorContext(ctx, "orphaned audio file after track delete",
				"event", "catalog.orphaned_audio",
				"track_id", trackId.String(),
				"user_id", userId.String(),
				"audio_ref", *audioRef,
				"queued_for_retry", queued,
				"error", err,
			)
			// Surface the partial deletion: the track row is gone but the audio
			// file is not, so the caller must not be told the delete fully
			// succeeded.
			return fmt.Errorf("%w: %w", ErrAudioOrphaned, err)
		}
	}

	return nil
}

// recordOrphan persists the orphan for the reconcile sweep and reports whether
// it was queued. A failure (including the table not existing yet) degrades to
// the log line and metric alone, never to a different client-visible error.
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
