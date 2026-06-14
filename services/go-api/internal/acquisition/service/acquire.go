package service

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
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
		slog.WarnContext(ctx, "acquire_track_not_found", "track_id", trackId.String())
		return nil
	}

	if track.AcquisitionStatus == domain.AcquisitionReady {
		if track.AudioRef != nil {
			exists, err := s.audioStore.Exists(ctx, *track.AudioRef)
			if err == nil && exists {
				slog.InfoContext(ctx, "acquire_skip_already_ready", "track_id", trackId.String())
				return nil
			}
			slog.InfoContext(ctx, "acquire_reacquire_missing_file",
				"track_id", trackId.String(), "audio_ref", *track.AudioRef)
		}
		track.RevertToPending()
		if err := s.trackRepo.Update(ctx, track); err != nil {
			return fmt.Errorf("revert to pending: %w", err)
		}
	}

	if track.AcquisitionStatus == domain.AcquisitionFailed {
		slog.InfoContext(ctx, "acquire_retrying_failed", "track_id", trackId.String())
		track.RevertToPending()
		if err := s.trackRepo.Update(ctx, track); err != nil {
			return fmt.Errorf("revert failed to pending: %w", err)
		}
	}

	slog.InfoContext(ctx, "track_acquisition_started",
		"track_id", trackId.String(),
		"user_id", userId.String(),
		"has_isrc", track.ISRC != nil,
	)

	dur := 0.0
	if track.DurationSeconds != nil {
		dur = *track.DurationSeconds
	}
	isrc := ""
	if track.ISRC != nil {
		isrc = *track.ISRC
	}

	year := 0
	if track.Year != nil {
		year = *track.Year
	}
	trackNum := 0
	if track.TrackNumber != nil {
		trackNum = *track.TrackNumber
	}
	albumArtist := ""
	if track.AlbumArtist != nil {
		albumArtist = *track.AlbumArtist
	}
	genre := ""
	if track.Genre != nil {
		genre = *track.Genre
	}

	ac := &AcquisitionContext{
		Track: TrackRef{
			ID:          track.ID.String(),
			UserID:      track.UserId.String(),
			Title:       track.Title,
			Artist:      track.Artist,
			Album:       track.Album,
			Duration:    dur,
			ISRC:        isrc,
			Year:        year,
			TrackNumber: trackNum,
			AlbumArtist: albumArtist,
			Genre:       genre,
		},
	}

	pipeline := []Step{
		NewSearchStep(s.audioSearcher),
		NewSelectStep(),
		NewDownloadStep(s.audioSearcher),
		NewTagStep(),
		NewStoreStep(s.audioStore),
		NewUpdateTrackStep(s.trackRepo, userId, trackId),
	}

	err = RunPipeline(ctx, pipeline, ac)
	cleanupTemp(ac)

	if err != nil {
		reason := err.Error()
		slog.WarnContext(ctx, "track_acquisition_failed",
			"track_id", trackId.String(),
			"user_id", userId.String(),
			"reason", reason,
		)
		s.markFailed(ctx, trackId, userId, reason)
		return err
	}

	slog.InfoContext(ctx, "track_acquisition_completed",
		"track_id", trackId.String(),
		"user_id", userId.String(),
		"audio_ref", ac.AudioRef,
	)
	return nil
}

func (s *AcquireTrackAudioService) markFailed(ctx context.Context, trackId domain.TrackId, userId shared.UserId, reason string) {
	track, err := s.trackRepo.GetByID(ctx, trackId, userId)
	if err != nil || track == nil {
		return
	}
	if markErr := track.MarkFailed(reason); markErr == nil {
		_ = s.trackRepo.Update(ctx, track)
	}
}

func cleanupTemp(ac *AcquisitionContext) {
	if ac.TempPath == "" {
		return
	}
	parent := filepath.Dir(ac.TempPath)
	if err := os.RemoveAll(parent); err != nil {
		slog.Warn("temp_cleanup_failed", "path", parent, "error", err)
	}
}
