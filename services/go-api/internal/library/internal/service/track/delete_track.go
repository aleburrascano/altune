package track

import (
	"altune/go-api/internal/library/internal/core/ports"
	"altune/go-api/internal/library/internal/domain"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
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

func NewDeleteTrackService(trackRepo ports.TrackAudioDeleter, audioStore ports.AudioStore, publisher events.Publisher, metrics ports.AudioStoreMetrics, orphans ports.OrphanedAudioRecorder) *DeleteTrackService {
	switch {
	case trackRepo == nil:
		panic("track: NewDeleteTrackService requires a track repository")
	case publisher == nil:
		panic("track: NewDeleteTrackService requires an events publisher")
	case metrics == nil:
		panic("track: NewDeleteTrackService requires audio store metrics")
	case orphans == nil:
		panic("track: NewDeleteTrackService requires an orphaned audio recorder")
	}
	return &DeleteTrackService{trackRepo: trackRepo, audioStore: audioStore, events: publisher, metrics: metrics, orphans: orphans}
}

func (s *DeleteTrackService) Execute(ctx context.Context, userId ids.UserId, trackId domain.TrackId) error {
	deleted, audioRef, err := s.trackRepo.Delete(ctx, trackId, userId)
	if err != nil {
		return domain.WrapRepoError(ctx, "delete track", err)
	}
	if !deleted {
		return domain.ErrTrackNotFound
	}

	slog.InfoContext(ctx, "track deleted from library",
		logging.KeyTrackID, trackId.String(), logging.KeyUserID, userId.String())
	s.events.Publish(ctx, userId, events.TrackDeleted{TrackID: trackId.String()})

	if audioRef == nil {
		return nil
	}
	return s.deleteAudio(ctx, userId, trackId, *audioRef)
}

func (s *DeleteTrackService) deleteAudio(ctx context.Context, userId ids.UserId, trackId domain.TrackId, audioRef string) error {
	inUse, err := s.trackRepo.AudioRefInUse(ctx, audioRef, trackId)
	if err != nil {
		return s.reportOrphanedAudio(ctx, userId, trackId, audioRef, fmt.Errorf("audio usage unknown: %w", err))
	}
	if inUse {
		slog.InfoContext(ctx, "audio kept: another track still references it",
			"event", "catalog.shared_audio_kept",
			logging.KeyTrackID, trackId.String(), "audio_ref", audioRef)
		return nil
	}
	if err := s.audioStore.Delete(ctx, audioRef); err != nil {
		return s.reportOrphanedAudio(ctx, userId, trackId, audioRef, err)
	}
	return nil
}

func (s *DeleteTrackService) reportOrphanedAudio(ctx context.Context, userId ids.UserId, trackId domain.TrackId, audioRef string, cause error) error {
	s.metrics.OrphanedDelete()
	queued := s.recordOrphan(ctx, userId, trackId, audioRef)
	slog.ErrorContext(ctx, "orphaned audio file after track delete",
		"event", "catalog.orphaned_audio",
		logging.KeyTrackID, trackId.String(),
		logging.KeyUserID, userId.String(),
		"audio_ref", audioRef,
		"queued_for_retry", queued,
		logging.KeyError, cause,
	)
	return fmt.Errorf("%w: %w", domain.ErrAudioOrphaned, cause)
}

func (s *DeleteTrackService) recordOrphan(ctx context.Context, userId ids.UserId, trackId domain.TrackId, audioRef string) bool {
	recordCtx, cancel := background.Detach(ctx, orphanRecordTimeout)
	defer cancel()
	err := s.orphans.RecordOrphanedAudio(recordCtx, ports.OrphanedAudio{AudioRef: audioRef, UserId: userId, TrackId: trackId})
	if err == nil {
		return true
	}
	level := slog.LevelError
	if errors.Is(err, ports.ErrOrphanedAudioQueueUnavailable) {
		level = slog.LevelWarn
	}
	slog.Log(ctx, level, "orphaned audio not queued for retry", "audio_ref", audioRef, logging.KeyError, err)
	return false
}
