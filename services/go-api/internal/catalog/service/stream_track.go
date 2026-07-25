package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
)

type StreamOutput struct {
	Reader ports.AudioStream
	Size   int64
	Track  *domain.Track
}

type streamTrackRepo interface {
	trackByIDGetter
	Update(ctx context.Context, track *domain.Track) error
}

type StreamTrackService struct {
	trackRepo  streamTrackRepo
	audioStore ports.AudioStore
	scheduler  ports.AcquisitionScheduler
}

func NewStreamTrackService(
	trackRepo streamTrackRepo,
	audioStore ports.AudioStore,
	opts ...func(*StreamTrackService),
) *StreamTrackService {
	s := &StreamTrackService{
		trackRepo:  trackRepo,
		audioStore: audioStore,
		scheduler:  ports.NoopAcquisitionScheduler(),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func WithStreamScheduler(scheduler ports.AcquisitionScheduler) func(*StreamTrackService) {
	return func(s *StreamTrackService) {
		if scheduler != nil {
			s.scheduler = scheduler
		}
	}
}

func (s *StreamTrackService) Execute(ctx context.Context, userId shared.UserId, trackId domain.TrackId) (*StreamOutput, error) {
	dbStart := time.Now()
	track, err := s.trackRepo.GetByID(ctx, trackId, userId)
	dbDuration := time.Since(dbStart)
	if err != nil {
		return nil, fmt.Errorf("stream track: %w", err)
	}
	if track == nil {
		return nil, ErrTrackNotFound
	}

	if !track.IsStreamable() {
		return nil, ErrAudioNotAvailable
	}

	storageStart := time.Now()
	reader, size, err := s.audioStore.Stream(ctx, *track.AudioRef)
	if err != nil {
		slog.WarnContext(ctx, "stream.audio_missing",
			"track_id", trackId.String(), "error", err)
		if recErr := s.recoverMissingAudio(ctx, userId, track); recErr != nil {
			slog.ErrorContext(ctx, "stream.recover_failed",
				"track_id", trackId.String(), "error", recErr)
		}
		return nil, ErrAudioNotAvailable
	}

	slog.InfoContext(ctx, "stream.opened",
		"track_id", trackId.String(),
		"db_lookup_ms", dbDuration.Milliseconds(),
		"storage_open_ms", time.Since(storageStart).Milliseconds(),
	)

	return &StreamOutput{Reader: reader, Size: size, Track: track}, nil
}

func (s *StreamTrackService) RecoverIfMissing(ctx context.Context, userId shared.UserId, trackId domain.TrackId) error {
	track, err := s.trackRepo.GetByID(ctx, trackId, userId)
	if err != nil {
		return fmt.Errorf("recover audio: %w", err)
	}
	if track == nil || !track.IsStreamable() {
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

func (s *StreamTrackService) recoverMissingAudio(ctx context.Context, userId shared.UserId, track *domain.Track) error {
	exists, err := s.audioStore.Exists(ctx, *track.AudioRef)
	return s.reconcileMissingAudio(ctx, userId, track, exists, err)
}

func (s *StreamTrackService) reconcileMissingAudio(ctx context.Context, userId shared.UserId, track *domain.Track, exists bool, err error) error {
	var recErr error
	switch {
	case err != nil:
		recErr = fmt.Errorf("audio existence check: %w", err)
	case !exists:
		if err := track.MarkFailed("audio file missing from storage"); err != nil {
			recErr = fmt.Errorf("mark failed: %w", err)
		} else {
			slog.WarnContext(ctx, "track marked failed: audio file missing",
				"track_id", track.ID.String(), "user_id", userId.String())
			if err := s.trackRepo.Update(ctx, track); err != nil {
				recErr = fmt.Errorf("persist recovery: %w", err)
			}
		}
	}

	slog.InfoContext(ctx, "stream.reacquire_scheduled",
		"track_id", track.ID.String())
	s.scheduler.Schedule(userId, track.ID, "")
	return recErr
}
