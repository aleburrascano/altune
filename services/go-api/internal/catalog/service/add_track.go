package service

import (
	"context"
	"encoding/json"
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
	FeaturedArtists []domain.FeaturedArtist
	SourceURL       *string
}

type AddTrackOutput struct {
	Track   *domain.Track
	Created bool
}

type AddTrackService struct {
	trackRepo ports.TrackRepository
	events    events.Publisher
	scheduler ports.AcquisitionScheduler
}

func NewAddTrackService(trackRepo ports.TrackRepository, opts ...func(*AddTrackService)) *AddTrackService {
	s := &AddTrackService{
		trackRepo: trackRepo,
		events:    events.NoopPublisher(),
		scheduler: ports.NoopAcquisitionScheduler(),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func WithAddTrackEvents(pub events.Publisher) func(*AddTrackService) {
	return func(s *AddTrackService) {
		if pub != nil {
			s.events = pub
		}
	}
}

func WithAcquisitionScheduler(scheduler ports.AcquisitionScheduler) func(*AddTrackService) {
	return func(s *AddTrackService) {
		if scheduler != nil {
			s.scheduler = scheduler
		}
	}
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
	track.FeaturedArtists = input.FeaturedArtists

	stored, created, err := s.trackRepo.Add(ctx, track)
	if err != nil {
		return nil, err
	}
	if stored != nil {
		track = stored
	}

	if created {
		slog.InfoContext(ctx, "track added to library",
			"track_id", track.ID.String(),
			"user_id", userId.String(),
		)
		s.events.Publish(userId, "track_added_to_library", trackAddedPayload(track))
		sourceURL := ""
		if input.SourceURL != nil {
			sourceURL = *input.SourceURL
		}
		slog.InfoContext(ctx, "acquisition.scheduled",
			"track_id", track.ID.String())
		s.scheduler.Schedule(userId, track.ID, sourceURL)
	}

	return &AddTrackOutput{Track: track, Created: created}, nil
}

func trackAddedPayload(t *domain.Track) map[string]any {
	dto := TrackToDTO(t)
	raw, _ := json.Marshal(dto)
	payload := map[string]any{}
	_ = json.Unmarshal(raw, &payload)
	payload["track_id"] = dto.ID.String()
	return payload
}
