package service

import (
	"context"
	"fmt"
	"log/slog"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/events"
)

type DeleteTrackService struct {
	trackRepo  ports.TrackRepository
	audioStore ports.AudioStore
	events     events.Publisher
}

func NewDeleteTrackService(trackRepo ports.TrackRepository, audioStore ports.AudioStore, opts ...func(*DeleteTrackService)) *DeleteTrackService {
	s := &DeleteTrackService{trackRepo: trackRepo, audioStore: audioStore}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func WithDeleteTrackEvents(pub events.Publisher) func(*DeleteTrackService) {
	return func(s *DeleteTrackService) { s.events = pub }
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

	if s.events != nil {
		s.events.Publish(userId, "track_deleted", map[string]any{
			"track_id": trackId.String(),
		})
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
