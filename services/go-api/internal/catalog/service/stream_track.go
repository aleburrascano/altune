package service

import (
	"context"
	"fmt"
	"log/slog"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
)

var ErrAudioNotAvailable = &domain.CodedError{Msg: "audio not available", Status: 404}

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
		s.recoverMissingAudio(ctx, userId, track)
		return nil, ErrAudioNotAvailable
	}

	return &StreamOutput{Reader: reader, Size: size, Track: track}, nil
}

// recoverMissingAudio reconciles a track whose audio failed to stream: if the
// file is genuinely gone from storage it is marked failed, and re-acquisition is
// scheduled regardless. The track is already loaded and known streamable here, so
// no second fetch or status re-check is needed — and any recovery error is logged
// at its source rather than swallowed.
func (s *StreamTrackService) recoverMissingAudio(ctx context.Context, userId shared.UserId, track *domain.Track) {
	exists, err := s.audioStore.Exists(ctx, *track.AudioRef)
	switch {
	case err != nil:
		slog.WarnContext(ctx, "stream.audio_check_failed",
			"track_id", track.ID.String(), "error", err)
	case !exists:
		if err := track.MarkFailed("audio file missing from storage"); err != nil {
			slog.ErrorContext(ctx, "stream.mark_failed_error",
				"track_id", track.ID.String(), "error", err)
		} else {
			slog.WarnContext(ctx, "track marked failed: audio file missing",
				"track_id", track.ID.String(), "user_id", userId.String())
			if err := s.trackRepo.Update(ctx, track); err != nil {
				slog.ErrorContext(ctx, "stream.reconcile_update_failed",
					"track_id", track.ID.String(), "error", err)
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
}
