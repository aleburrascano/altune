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
	metrics    ports.AudioStoreMetrics
}

func NewDeleteTrackService(trackRepo ports.TrackRepository, audioStore ports.AudioStore, opts ...func(*DeleteTrackService)) *DeleteTrackService {
	s := &DeleteTrackService{trackRepo: trackRepo, audioStore: audioStore, events: events.NoopPublisher(), metrics: ports.NoopAudioStoreMetrics()}
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

func WithDeleteTrackMetrics(m ports.AudioStoreMetrics) func(*DeleteTrackService) {
	return func(s *DeleteTrackService) {
		if m != nil {
			s.metrics = m
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
			s.metrics.OrphanedDelete()
			slog.ErrorContext(ctx, "orphaned audio file after track delete",
				"track_id", trackId.String(),
				"audio_ref", *audioRef,
				"error", err,
			)
		}
	}

	return nil
}
