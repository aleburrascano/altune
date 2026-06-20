package service

import (
	"context"
	"log/slog"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/events"
)

type AddTrackInput struct {
	Title           string
	Artist          string
	Album           string
	DurationSeconds *float64
	ArtworkURL      *string
	Year            *int
	Genre           *string
	TrackNumber     *int
	AlbumArtist     *string
	ISRC            *string
}

type AddTrackOutput struct {
	Track   *domain.Track
	Created bool
}

type AddTrackService struct {
	trackRepo ports.TrackRepository
	events    events.Publisher
}

func NewAddTrackService(trackRepo ports.TrackRepository, opts ...func(*AddTrackService)) *AddTrackService {
	s := &AddTrackService{trackRepo: trackRepo}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func WithAddTrackEvents(pub events.Publisher) func(*AddTrackService) {
	return func(s *AddTrackService) { s.events = pub }
}

func (s *AddTrackService) Execute(ctx context.Context, userId shared.UserId, input AddTrackInput) (*AddTrackOutput, error) {
	track, err := domain.NewTrack(userId, input.Title, input.Artist, input.Album)
	if err != nil {
		return nil, err
	}
	track.DurationSeconds = input.DurationSeconds
	track.ArtworkURL = input.ArtworkURL
	track.Year = input.Year
	track.Genre = input.Genre
	track.TrackNumber = input.TrackNumber
	track.AlbumArtist = input.AlbumArtist
	track.ISRC = input.ISRC

	created, err := s.trackRepo.Add(ctx, track)
	if err != nil {
		return nil, err
	}

	if created {
		slog.InfoContext(ctx, "track added to library",
			"track_id", track.ID.String(),
			"user_id", userId.String(),
		)
		if s.events != nil {
			s.events.Publish(userId, "track_added_to_library", map[string]any{
				"track_id": track.ID.String(),
			})
		}
	}

	if !created {
		existing, err := s.trackRepo.GetByDedupKey(ctx, userId, track.DedupKey)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			track = existing
		}
	}

	return &AddTrackOutput{Track: track, Created: created}, nil
}
