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
	// SourceURL is a transient acquisition hint (e.g. a SoundCloud permalink),
	// not a domain attribute — it is never written to the Track. When set and the
	// track is freshly created, it is forwarded to the acquisition scheduler so
	// acquisition grabs that exact source instead of re-searching by metadata.
	SourceURL *string
}

type AddTrackOutput struct {
	Track   *domain.Track
	Created bool
}

// trackAdder is the narrow write this service actually calls, out of
// ports.TrackRepository's full surface.
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
		s.events.Publish(userId, "track_added_to_library", trackAddedPayload(ctx, track))
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

// trackAddedPayload builds the full track object embedded in the
// track_added_to_library event (F10), so a receiving client inserts the row
// directly instead of forcing a refetch. It marshals the same TrackDTO the HTTP
// handler serializes and re-opens it as the bus's map payload, so the event can
// never drift from the wire shape — they are the same struct.
func trackAddedPayload(ctx context.Context, t *domain.Track) map[string]any {
	// A DTO of plain values cannot fail to (un)marshal; logged rather than
	// silently dropped in case that assumption is ever wrong.
	b, err := json.Marshal(TrackToDTO(t))
	if err != nil {
		slog.ErrorContext(ctx, "track_added_to_library payload marshal failed", "track_id", t.ID.String(), "error", err)
		return map[string]any{"track_id": t.ID.String()}
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		slog.ErrorContext(ctx, "track_added_to_library payload unmarshal failed", "track_id", t.ID.String(), "error", err)
		return map[string]any{"track_id": t.ID.String()}
	}
	m["track_id"] = t.ID.String() // retained for older clients
	return m
}
