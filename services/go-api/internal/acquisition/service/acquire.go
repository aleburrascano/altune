package service

import (
	"context"
	"fmt"
	"log/slog"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"

	"altune/go-api/internal/acquisition/service/steps"
)

type AcquireTrackAudioService struct {
	trackRepo     ports.TrackRepository
	audioSearcher ports.AudioSearcher
	audioStore    ports.AudioStore
}

func NewAcquireTrackAudioService(
	trackRepo ports.TrackRepository,
	audioSearcher ports.AudioSearcher,
	audioStore ports.AudioStore,
) *AcquireTrackAudioService {
	return &AcquireTrackAudioService{
		trackRepo:     trackRepo,
		audioSearcher: audioSearcher,
		audioStore:    audioStore,
	}
}

func (s *AcquireTrackAudioService) Execute(ctx context.Context, userId shared.UserId, trackId domain.TrackId) error {
	track, err := s.trackRepo.GetByID(ctx, trackId, userId)
	if err != nil {
		return fmt.Errorf("get track: %w", err)
	}
	if track == nil {
		return fmt.Errorf("track %s not found", trackId)
	}

	if track.AcquisitionStatus == domain.AcquisitionReady {
		return nil
	}

	dur := 0.0
	if track.DurationSeconds != nil {
		dur = *track.DurationSeconds
	}

	isrc := ""
	if track.ISRC != nil {
		isrc = *track.ISRC
	}

	ac := &AcquisitionContext{
		Track: TrackRef{
			ID:       track.ID.String(),
			UserID:   track.UserId.String(),
			Title:    track.Title,
			Artist:   track.Artist,
			Album:    track.Album,
			Duration: dur,
			ISRC:     isrc,
		},
	}

	pipeline := []Step{
		steps.NewSearchStep(s.audioSearcher),
		steps.NewSelectStep(),
		steps.NewDownloadStep(s.audioSearcher),
		steps.NewStoreStep(s.audioStore),
		steps.NewUpdateTrackStep(s.trackRepo, userId, trackId),
	}

	if err := RunPipeline(ctx, pipeline, ac); err != nil {
		slog.ErrorContext(ctx, "acquisition failed, marking track as failed",
			"track_id", trackId.String(), "error", err)

		if markErr := track.MarkFailed(err.Error()); markErr == nil {
			_ = s.trackRepo.Update(ctx, track)
		}

		return err
	}

	slog.InfoContext(ctx, "acquisition completed",
		"track_id", trackId.String(), "audio_ref", ac.AudioRef)
	return nil
}
