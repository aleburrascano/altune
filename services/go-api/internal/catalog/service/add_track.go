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
	FeaturedArtists []domain.FeaturedArtist
	SourceURL       *string
}

type AddTrackOutput struct {
	Track   *domain.Track
	Created bool
}

type trackAdder interface {
	Add(ctx context.Context, track *domain.Track) (stored *domain.Track, created bool, err error)
}

type AddTrackService struct {
	trackRepo trackAdder
	events    events.Publisher
	scheduler ports.AcquisitionScheduler
}

func NewAddTrackService(trackRepo trackAdder, opts ...func(*AddTrackService)) *AddTrackService {
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
	m := map[string]any{
		"id":                 dto.ID.String(),
		"track_id":           dto.ID.String(),
		"title":              dto.Title,
		"artist":             dto.Artist,
		"album":              nullablePayloadField(dto.Album),
		"duration_seconds":   nullablePayloadField(dto.DurationSeconds),
		"added_at":           dto.AddedAt,
		"acquisition_status": dto.AcquisitionStatus,
		"artwork_url":        nullablePayloadField(dto.ArtworkURL),
	}
	setPayloadFieldIfPresent(m, "year", dto.Year)
	setPayloadFieldIfPresent(m, "genre", dto.Genre)
	setPayloadFieldIfPresent(m, "track_number", dto.TrackNumber)
	setPayloadFieldIfPresent(m, "album_artist", dto.AlbumArtist)
	setPayloadFieldIfPresent(m, "isrc", dto.ISRC)
	setPayloadFieldIfPresent(m, "audio_ref", dto.AudioRef)
	setPayloadFieldIfPresent(m, "failure_reason", dto.FailureReason)
	setPayloadFieldIfPresent(m, "failure_message", dto.FailureMessage)
	if feats := featuredArtistsPayload(dto.FeaturedArtists); feats != nil {
		m["featured_artists"] = feats
	}
	return m
}

func nullablePayloadField[T any](p *T) any {
	if p == nil {
		return nil
	}
	return *p
}

func setPayloadFieldIfPresent[T any](m map[string]any, key string, p *T) {
	if p != nil {
		m[key] = *p
	}
}

func featuredArtistsPayload(feats []FeaturedArtistDTO) []map[string]any {
	if len(feats) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(feats))
	for _, f := range feats {
		entry := map[string]any{"name": f.Name}
		setPayloadFieldIfPresent(entry, "mbid", f.MBID)
		setPayloadFieldIfPresent(entry, "deezer_id", f.DeezerID)
		out = append(out, entry)
	}
	return out
}
