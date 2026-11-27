package audio

import (
	"altune/go-api/internal/library/internal/core/ports"
	"altune/go-api/internal/library/internal/domain"
	"altune/go-api/internal/library/internal/service/track"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/ids"
	"context"
	"errors"
	"fmt"
	"log/slog"
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
	now             clock.Func
}

func NewStreamTrackService(
	trackRepo ports.TrackReadWriter,
	audioStore ports.AudioStore,
	scheduler ports.AcquisitionScheduler,
	metrics ports.AudioStoreMetrics,
	recoveryEnabled func() bool,
	now clock.Func,
) *StreamTrackService {
	if scheduler == nil || metrics == nil || recoveryEnabled == nil || now == nil {
		panic("audio: NewStreamTrackService requires a scheduler, metrics, a recovery switch and a clock")
	}
	return &StreamTrackService{
		trackRepo:       trackRepo,
		audioStore:      audioStore,
		scheduler:       scheduler,
		metrics:         metrics,
		recoveryEnabled: recoveryEnabled,
		now:             now,
	}
}

func AlwaysEnabled() bool { return true }

func (s *StreamTrackService) Execute(ctx context.Context, userId ids.UserId, trackId domain.TrackId) (*StreamOutput, error) {
	dbStart := s.now()
	loaded, err := s.trackRepo.GetByID(ctx, trackId, userId)
	dbDuration := s.now().Sub(dbStart)
	if errors.Is(err, domain.ErrTrackNotFound) {
		return nil, domain.ErrTrackNotFound
	}
	if err != nil {
		return nil, domain.WrapRepoError(ctx, "stream track", err)
	}

	if !loaded.IsStreamable() {
		return nil, domain.ErrAudioNotAvailable
	}

	storageStart := s.now()
	reader, size, err := s.audioStore.Stream(ctx, *loaded.AudioRef)
	if err != nil {
		return nil, s.audioOpenFailure(ctx, userId, loaded, err)
	}

	slog.InfoContext(ctx, "stream.opened",
		"track_id", trackId.String(),
		"db_lookup_ms", dbDuration.Milliseconds(),
		"storage_open_ms", s.now().Sub(storageStart).Milliseconds(),
	)

	return &StreamOutput{Reader: reader, Size: size, Track: loaded}, nil
}

func (s *StreamTrackService) audioOpenFailure(ctx context.Context, userId ids.UserId, loaded *domain.Track, openErr error) error {
	trackId := loaded.ID
	slog.WarnContext(ctx, "stream.audio_missing",
		"track_id", trackId.String(), "error", openErr)
	if !s.recoveryEnabled() {
		slog.WarnContext(ctx, "stream.recovery_disabled", "track_id", trackId.String())
		return domain.ErrAudioTemporarilyUnavailable
	}
	s.metrics.StreamRecoveryTriggered()
	confirmedMissing, recErr := s.recoverMissingAudio(ctx, userId, loaded)
	if recErr != nil {
		slog.ErrorContext(ctx, "stream.recover_failed",
			"track_id", trackId.String(), "error", recErr)
	}
	if confirmedMissing {
		return domain.ErrAudioNotAvailable
	}
	return domain.ErrAudioTemporarilyUnavailable
}

func (s *StreamTrackService) RecoverIfMissing(ctx context.Context, userId ids.UserId, trackId domain.TrackId) error {
	loaded, err := s.trackRepo.GetByID(ctx, trackId, userId)
	if errors.Is(err, domain.ErrTrackNotFound) {
		return domain.ErrTrackNotFound
	}
	if err != nil {
		return domain.WrapRepoError(ctx, "recover audio", err)
	}
	if !loaded.IsStreamable() {
		return nil
	}
	if !s.recoveryEnabled() {
		slog.WarnContext(ctx, "stream.recovery_disabled", "track_id", trackId.String())
		return nil
	}

	exists, err := s.audioStore.Exists(ctx, *loaded.AudioRef)
	if err != nil {
		return fmt.Errorf("recover audio: exists check: %w", err)
	}
	if exists {
		return nil
	}
	return s.markMissingAndReacquire(ctx, userId, loaded)
}

func (s *StreamTrackService) recoverMissingAudio(ctx context.Context, userId ids.UserId, current *domain.Track) (confirmedMissing bool, err error) {
	exists, err := s.audioStore.Exists(ctx, *current.AudioRef)
	if err != nil {
		return false, fmt.Errorf("audio existence check: %w", err)
	}
	if exists {
		return false, nil
	}
	return true, s.markMissingAndReacquire(ctx, userId, current)
}

func (s *StreamTrackService) markMissingAndReacquire(ctx context.Context, userId ids.UserId, current *domain.Track) error {
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
