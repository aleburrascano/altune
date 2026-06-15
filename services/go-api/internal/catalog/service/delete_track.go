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

var ErrTrackNotFound = errors.New("track not found")

type DeleteTrackService struct {
	trackRepo  ports.TrackRepository
	audioStore ports.AudioStore
}

func NewDeleteTrackService(trackRepo ports.TrackRepository, audioStore ports.AudioStore) *DeleteTrackService {
	return &DeleteTrackService{trackRepo: trackRepo, audioStore: audioStore}
}

func (s *DeleteTrackService) Execute(ctx context.Context, userId shared.UserId, trackId domain.TrackId) error {
	track, err := s.trackRepo.GetByID(ctx, trackId, userId)
	if err != nil {
		return fmt.Errorf("get track for delete: %w", err)
	}
	if track == nil {
		return ErrTrackNotFound
	}

	audioRef := track.AudioRef

	deleted, err := s.trackRepo.Delete(ctx, trackId, userId)
	if err != nil {
		return fmt.Errorf("delete track: %w", err)
	}
	if !deleted {
		return ErrTrackNotFound
	}

	if audioRef != nil && s.audioStore != nil {
		if err := s.audioStore.Delete(ctx, *audioRef); err != nil {
			slog.ErrorContext(ctx, "orphaned audio file after track delete",
				"track_id", trackId.String(),
				"audio_ref", *audioRef,
				"error", err,
			)
		}
	}

	return nil
}
