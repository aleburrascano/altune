package audio

import (
	"altune/go-api/internal/library/domain"
	"altune/go-api/internal/library/internal/core/ports"
	"altune/go-api/internal/library/service/track"
	"altune/go-api/internal/shared/ids"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

type StreamOutput struct {
	Reader ports.AudioStream
	Size   int64
	Track  *domain.Track
}

type StreamTrackService struct {
	trackRepo       ports.TrackReadWriter
	audioStore      ports.AudioStore
	scheduler       ports.AcquisitionScheduler
	metrics         ports.AudioStoreMetrics
	recoveryEnabled func() bool
}

func NewStreamTrackService(
	trackRepo ports.TrackReadWriter,
	audioStore ports.AudioStore,
	opts ...func(*StreamTrackService),
) *StreamTrackService {
	s := &StreamTrackService{
		trackRepo:  trackRepo,
		audioStore: audioStore,
		scheduler:  ports.NoopAcquisitionScheduler(),
		metrics:    ports.NoopAudioStoreMetrics(),

		recoveryEnabled: func() bool { return true },
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func WithStreamRecoverySwitch(enabled func() bool) func(*StreamTrackService) {
	return func(s *StreamTrackService) {
		if enabled != nil {
			s.recoveryEnabled = enabled
		}
	}
}

func WithStreamScheduler(scheduler ports.AcquisitionScheduler) func(*StreamTrackService) {
	return func(s *StreamTrackService) {
		if scheduler != nil {
			s.scheduler = scheduler
		}
	}
}

func WithStreamMetrics(m ports.AudioStoreMetrics) func(*StreamTrackService) {
	return func(s *StreamTrackService) {
		if m != nil {
			s.metrics = m
		}
	}
}

func (s *StreamTrackService) Execute(ctx context.Context, userId ids.UserId, trackId domain.TrackId) (*StreamOutput, error) {
	dbStart := time.Now()
	track, err := s.trackRepo.GetByID(ctx, trackId, userId)
	dbDuration := time.Since(dbStart)
	if errors.Is(err, domain.ErrTrackNotFound) {
		return nil, domain.ErrTrackNotFound
	}
	if err != nil {
		return nil, domain.WrapRepoError(ctx, "stream track", err)
	}

	if !track.IsStreamable() {
		return nil, domain.ErrAudioNotAvailable
	}

	storageStart := time.Now()
	reader, size, err := s.audioStore.Stream(ctx, *track.AudioRef)
	if err != nil {
		slog.WarnContext(ctx, "stream.audio_missing",
			"track_id", trackId.String(), "error", err)
		if !s.recoveryEnabled() {
			slog.WarnContext(ctx, "stream.recovery_disabled", "track_id", trackId.String())
			return nil, domain.ErrAudioTemporarilyUnavailable
		}
		s.metrics.StreamRecoveryTriggered()
		confirmedMissing, recErr := s.recoverMissingAudio(ctx, userId, track)
		if recErr != nil {
			slog.ErrorContext(ctx, "stream.recover_failed",
				"track_id", trackId.String(), "error", recErr)
		}
		if confirmedMissing {
			return nil, domain.ErrAudioNotAvailable
		}
		return nil, domain.ErrAudioTemporarilyUnavailable
	}

	slog.InfoContext(ctx, "stream.opened",
		"track_id", trackId.String(),
		"db_lookup_ms", dbDuration.Milliseconds(),
		"storage_open_ms", time.Since(storageStart).Milliseconds(),
	)

	return &StreamOutput{Reader: reader, Size: size, Track: track}, nil
}

func (s *StreamTrackService) RecoverIfMissing(ctx context.Context, userId ids.UserId, trackId domain.TrackId) error {
	track, err := s.trackRepo.GetByID(ctx, trackId, userId)
	if errors.Is(err, domain.ErrTrackNotFound) {
		return domain.ErrTrackNotFound
	}
	if err != nil {
		return domain.WrapRepoError(ctx, "recover audio", err)
	}
	if !track.IsStreamable() {
		return nil
	}
	if !s.recoveryEnabled() {
		slog.WarnContext(ctx, "stream.recovery_disabled", "track_id", trackId.String())
		return nil
	}

	exists, err := s.audioStore.Exists(ctx, *track.AudioRef)
	if err != nil {
		return fmt.Errorf("recover audio: exists check: %w", err)
	}
	if exists {
		return nil
	}
	return s.reconcileMissingAudio(ctx, userId, track, false, nil)
}

func (s *StreamTrackService) recoverMissingAudio(ctx context.Context, userId ids.UserId, current *domain.Track) (confirmedMissing bool, err error) {
	exists, existsErr := s.audioStore.Exists(ctx, *current.AudioRef)
	recErr := s.reconcileMissingAudio(ctx, userId, current, exists, existsErr)
	return existsErr == nil && !exists, recErr
}

func (s *StreamTrackService) reconcileMissingAudio(ctx context.Context, userId ids.UserId, current *domain.Track, exists bool, err error) error {
	if err != nil {
		return fmt.Errorf("audio existence check: %w", err)
	}
	if exists {
		return nil
	}
	if err := s.markAudioMissing(ctx, userId, current); err != nil {
		return err
	}
	s.scheduleReacquire(ctx, userId, current.ID)
	return nil
}

func (s *StreamTrackService) markAudioMissing(ctx context.Context, userId ids.UserId, current *domain.Track) error {
	expectedVersion := current.Version
	if err := current.MarkFailed("audio file missing from storage"); err != nil {
		return fmt.Errorf("mark failed: %w", err)
	}
	slog.WarnContext(ctx, "track marked failed: audio file missing",
		"track_id", current.ID.String(), "user_id", userId.String())
	if err := s.trackRepo.Update(ctx, current, expectedVersion); err != nil {
		return fmt.Errorf("persist recovery: %w", err)
	}
	return nil
}

func (s *StreamTrackService) scheduleReacquire(ctx context.Context, userId ids.UserId, trackId domain.TrackId) {
	slog.InfoContext(ctx, "stream.reacquire_scheduled",
		"track_id", trackId.String())
	if err := track.ScheduleBounded(ctx, s.scheduler, userId, trackId, ""); err != nil {
		slog.WarnContext(ctx, "stream.reacquire_refused",
			"track_id", trackId.String(), "error", err)
	}
}
