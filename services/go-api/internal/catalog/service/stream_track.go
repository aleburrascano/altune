package service

import (
	"context"
	"fmt"
	"log/slog"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
)

type StreamOutput struct {
	Reader ports.AudioStream
	Size   int64
	Track  *domain.Track
}

type StreamTrackService struct {
	trackRepo  ports.TrackRepository
	audioStore ports.AudioStore
	scheduler  ports.AcquisitionScheduler
}

func NewStreamTrackService(
	trackRepo ports.TrackRepository,
	audioStore ports.AudioStore,
	scheduler ports.AcquisitionScheduler,
) *StreamTrackService {
	return &StreamTrackService{
		trackRepo:  trackRepo,
		audioStore: audioStore,
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
		if recErr := s.recoverMissingAudio(ctx, userId, track); recErr != nil {
			slog.ErrorContext(ctx, "stream.recover_failed",
				"track_id", trackId.String(), "error", recErr)
		}
		return nil, ErrAudioNotAvailable
	}

	return &StreamOutput{Reader: reader, Size: size, Track: track}, nil
}

// recoverMissingAudio reconciles a track whose audio failed to stream: if the
// file is genuinely gone from storage it is marked failed, and re-acquisition is
// scheduled regardless. The track is already loaded and known streamable here, so
// no second fetch or status re-check is needed. Reconcile failures are returned
// (the caller logs once) rather than logged-and-swallowed here; scheduling stays
// fire-and-forget and runs whether or not reconcile succeeded.
func (s *StreamTrackService) recoverMissingAudio(ctx context.Context, userId shared.UserId, track *domain.Track) error {
	var recErr error
	exists, err := s.audioStore.Exists(ctx, *track.AudioRef)
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

	if s.scheduler != nil {
		slog.InfoContext(ctx, "stream.reacquire_scheduled",
			"track_id", track.ID.String())
		// Re-acquisition has no source URL (triggered by a missing file), so it
		// falls back to the search pipeline.
		s.scheduler.Schedule(userId, track.ID, "")
	}
	return recErr
}
