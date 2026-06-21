package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
)

var ErrAudioNotAvailable = errors.New("audio not available")

type StreamOutput struct {
	Reader ports.AudioStream
	Size   int64
	Track  *domain.Track
}

type StreamTrackService struct {
	trackRepo  ports.TrackRepository
	audioStore ports.AudioStore
	reconcile  *ReconcileTrackStatusService
	scheduler  ports.ReacquisitionScheduler
}

func NewStreamTrackService(
	trackRepo ports.TrackRepository,
	audioStore ports.AudioStore,
	reconcile *ReconcileTrackStatusService,
	scheduler ports.ReacquisitionScheduler,
) *StreamTrackService {
	return &StreamTrackService{
		trackRepo:  trackRepo,
		audioStore: audioStore,
		reconcile:  reconcile,
		scheduler:  scheduler,
	}
}

func (s *StreamTrackService) Execute(ctx context.Context, userId shared.UserId, trackId domain.TrackId) (*StreamOutput, error) {
	track, err := s.trackRepo.GetByID(ctx, trackId, userId)
	if err != nil {
		return nil, fmt.Errorf("stream track: %w", err)
	}
	if track == nil {
		return nil, ErrTrackNotFound
	}

	if !track.IsStreamable() {
		return nil, ErrAudioNotAvailable
	}

	reader, size, err := s.audioStore.Stream(ctx, *track.AudioRef)
	if err != nil {
		slog.WarnContext(ctx, "stream.audio_missing",
			"track_id", trackId.String(), "error", err)

		_ = s.reconcile.Execute(ctx, userId, trackId)

		if s.scheduler != nil {
			slog.InfoContext(ctx, "stream.reacquire_scheduled",
				"track_id", trackId.String())
			// Re-acquisition has no source URL (triggered by a missing file), so
			// it falls back to the search pipeline.
			s.scheduler.Schedule(userId, trackId, "")
		}

		return nil, ErrAudioNotAvailable
	}

	return &StreamOutput{Reader: reader, Size: size, Track: track}, nil
}
