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
