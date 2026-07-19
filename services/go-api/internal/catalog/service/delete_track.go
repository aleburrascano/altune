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

// trackDeleter is the narrow write this service actually calls, out of
// ports.TrackRepository's full surface.
type trackDeleter interface {
	Delete(ctx context.Context, id domain.TrackId, userId shared.UserId) (deleted bool, audioRef *string, err error)
}

type DeleteTrackService struct {
	trackRepo  trackDeleter
	audioStore ports.AudioStore
	events     events.Publisher
}

func NewDeleteTrackService(trackRepo trackDeleter, audioStore ports.AudioStore, opts ...func(*DeleteTrackService)) *DeleteTrackService {
	s := &DeleteTrackService{trackRepo: trackRepo, audioStore: audioStore, events: events.NoopPublisher()}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func WithDeleteTrackEvents(pub events.Publisher) func(*DeleteTrackService) {
	return func(s *DeleteTrackService) {
		if pub != nil {
			s.events = pub
		}
	}
}

func (s *DeleteTrackService) Execute(ctx context.Context, userId shared.UserId, trackId domain.TrackId) error {
	deleted, audioRef, err := s.trackRepo.Delete(ctx, trackId, userId)
	if err != nil {
		return fmt.Errorf("delete track: %w", err)
	}
	if !deleted {
		return ErrTrackNotFound
	}

	s.events.Publish(userId, "track_deleted", map[string]any{
		"track_id": trackId.String(),
	})

	if audioRef != nil {
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
