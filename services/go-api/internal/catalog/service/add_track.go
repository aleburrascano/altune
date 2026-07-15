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

type AddTrackService struct {
	trackRepo ports.TrackRepository
	events    events.Publisher
	scheduler ports.AcquisitionScheduler
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

func WithAcquisitionScheduler(scheduler ports.AcquisitionScheduler) func(*AddTrackService) {
	return func(s *AddTrackService) { s.scheduler = scheduler }
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
		if s.events != nil {
			s.events.Publish(userId, "track_added_to_library", trackAddedPayload(track))
		}
		if s.scheduler != nil {
			sourceURL := ""
			if input.SourceURL != nil {
				sourceURL = *input.SourceURL
			}
			slog.InfoContext(ctx, "acquisition.scheduled",
				"track_id", track.ID.String())
			s.scheduler.Schedule(userId, track.ID, sourceURL)
		}
	}

	return &AddTrackOutput{Track: track, Created: created}, nil
}

// trackAddedPayload builds the full track object embedded in the
// track_added_to_library event (F10), so a receiving client inserts the row
// directly instead of forcing a refetch. The JSON keys mirror the handler's
// TrackResponse wire shape — kept in sync with trackToResponse. `album` is null
// when empty, matching the wire contract.
func trackAddedPayload(t *domain.Track) map[string]any {
	var album *string
	if t.Album != "" {
		a := t.Album
		album = &a
	}
	return map[string]any{
		"track_id":           t.ID.String(), // retained for older clients
		"id":                 t.ID.String(),
		"title":              t.Title,
		"artist":             t.Artist,
		"album":              album,
		"duration_seconds":   t.DurationSeconds,
		"added_at":           t.AddedAt,
		"acquisition_status": t.AcquisitionStatus.String(),
		"artwork_url":        t.ArtworkURL,
		"year":               t.Year,
		"genre":              t.Genre,
		"track_number":       t.TrackNumber,
		"album_artist":       t.AlbumArtist,
		"isrc":               t.ISRC,
		"audio_ref":          t.AudioRef,
		"failure_reason":     t.FailureReason,
		"featured_artists":   featuredArtistsPayload(t.FeaturedArtists),
	}
}

// featuredArtistsPayload mirrors the handler's FeaturedArtistDTO wire shape for
// the embedded event, omitting empty ids. Returns nil for no credits.
func featuredArtistsPayload(feats []domain.FeaturedArtist) []map[string]any {
	if len(feats) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(feats))
	for _, f := range feats {
		m := map[string]any{"name": f.Name}
		if f.MBID != "" {
			m["mbid"] = f.MBID
		}
		if f.DeezerID != 0 {
			m["deezer_id"] = f.DeezerID
		}
		out = append(out, m)
	}
	return out
}
