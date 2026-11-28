package service

import (
	"altune/go-api/internal/library/internal/domain"
	"altune/go-api/internal/library/internal/ports"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

const MaxFeaturedArtistsPerTrack = domain.MaxFeaturedArtistsPerTrack

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
	IdempotencyKey  *string
	CatalogMBID     *string
	CatalogExplicit *bool
	CatalogSources  []domain.CatalogSource
}

type AddTrackOutput struct {
	Track   *domain.Track
	Created bool
}

type AddTrackService struct {
	trackRepo ports.TrackAddUpdater
	events    ports.TrackEvents
	scheduler ports.AcquisitionScheduler
	now       func() time.Time
	source    ids.Source
}

func NewAddTrackService(trackRepo ports.TrackAddUpdater, publisher ports.TrackEvents, scheduler ports.AcquisitionScheduler, now func() time.Time, source ids.Source) (*AddTrackService, error) {
	switch {
	case trackRepo == nil:
		return nil, fmt.Errorf("NewAddTrackService: the track repository: %w", domain.ErrTrackCollaboratorRequired)
	case publisher == nil:
		return nil, fmt.Errorf("NewAddTrackService: the events publisher: %w", domain.ErrTrackCollaboratorRequired)
	case scheduler == nil:
		return nil, fmt.Errorf("NewAddTrackService: the acquisition scheduler: %w", domain.ErrTrackCollaboratorRequired)
	case now == nil:
		return nil, fmt.Errorf("NewAddTrackService: %w", domain.ErrTrackClockRequired)
	case source == nil:
		return nil, fmt.Errorf("NewAddTrackService: %w", domain.ErrTrackIDSourceRequired)
	}
	return &AddTrackService{trackRepo: trackRepo, events: publisher, scheduler: scheduler, now: now, source: source}, nil
}

func MustNewAddTrackService(trackRepo ports.TrackAddUpdater, publisher ports.TrackEvents, scheduler ports.AcquisitionScheduler, now func() time.Time, source ids.Source) *AddTrackService {
	s, err := NewAddTrackService(trackRepo, publisher, scheduler, now, source)
	if err != nil {
		panic(err)
	}
	return s
}

func (s *AddTrackService) Execute(ctx context.Context, userId ids.UserId, input AddTrackInput) (*AddTrackOutput, error) {
	if err := validateAddTrackInput(input, s.now()); err != nil {
		return nil, err
	}
	track, err := domain.NewTrack(domain.NewTrackInput{
		UserId:          userId,
		Title:           input.Title,
		Artist:          input.Artist,
		Album:           input.Album,
		ArtworkURL:      input.ArtworkURL,
		Year:            input.Year,
		Genre:           input.Genre,
		TrackNumber:     input.TrackNumber,
		ISRC:            input.ISRC,
		FeaturedArtists: input.FeaturedArtists,
		IdempotencyKey:  input.IdempotencyKey,
	}, s.now, s.source)
	if err != nil {
		return nil, fmt.Errorf("new track: %w", err)
	}
	if err := whenGiven(input.DurationSeconds, track.SetCatalogDuration); err != nil {
		return nil, err
	}
	sources := input.CatalogSources
	if input.SourceURL != nil {
		sources = domain.AttachSoundCloudURL(sources, *input.SourceURL)
	}
	if err := track.SetCatalogIdentity(input.CatalogMBID, input.CatalogExplicit, sources); err != nil {
		return nil, fmt.Errorf("set catalog identity: %w", err)
	}
	if input.AlbumArtist != nil {
		track.SetAlbumArtist(*input.AlbumArtist)
	}

	stored, created, err := s.trackRepo.Add(ctx, track)
	if errors.Is(err, domain.ErrLibraryFull) {
		slog.WarnContext(ctx, "catalog.library_full", "cap", domain.MaxTracksPerUser)
		return nil, err
	}
	if err != nil {
		return nil, wrapRepoError(ctx, "add track", err)
	}
	if stored != nil {
		track = stored
	}

	if !created {
		return &AddTrackOutput{Track: track, Created: false}, nil
	}
	slog.InfoContext(ctx, "track added to library",
		logging.KeyTrackID, track.ID().String(),
	)
	s.events.TrackAdded(ctx, userId, track)
	sourceURL := ""
	if input.SourceURL != nil {
		sourceURL = *input.SourceURL
	}
	s.scheduleAcquisition(ctx, userId, track, sourceURL)
	return &AddTrackOutput{Track: track, Created: true}, nil
}

const ScheduleTimeout = 5 * time.Second

func ScheduleBounded(ctx context.Context, scheduler ports.AcquisitionScheduler, userId ids.UserId, trackId domain.TrackId, sourceURL string) error {
	ctx, cancel := context.WithTimeout(ctx, ScheduleTimeout)
	defer cancel()
	return scheduler.Schedule(ctx, userId, trackId, sourceURL)
}

func (s *AddTrackService) scheduleAcquisition(ctx context.Context, userId ids.UserId, track *domain.Track, sourceURL string) {
	slog.InfoContext(ctx, "acquisition.scheduled", logging.KeyTrackID, track.ID().String())
	schedErr := ScheduleBounded(ctx, s.scheduler, userId, track.ID(), sourceURL)
	if schedErr == nil {
		return
	}
	slog.WarnContext(ctx, "acquisition.schedule_refused",
		logging.KeyTrackID, track.ID().String(), logging.KeyError, schedErr)
	failed := *track
	expectedVersion := failed.Version()
	_ = failed.MarkFailed(string(domain.FailureAcquisitionRefused))
	if err := s.trackRepo.Update(ctx, &failed, expectedVersion); err != nil {
		slog.ErrorContext(ctx, "acquisition.schedule_refused_persist_failed",
			logging.KeyTrackID, track.ID().String(), logging.KeyError, err)
		return
	}
	*track = failed
	s.events.Publish(ctx, userId, events.TrackAcquisitionFailed{
		TrackID: track.ID().String(),
		Reason:  string(domain.FailureAcquisitionRefused),
	})
}

func whenGiven[T any](value *T, apply func(T) error) error {
	if value == nil {
		return nil
	}
	return apply(*value)
}

func validateAddTrackNumbers(input AddTrackInput, now time.Time) error {
	if err := whenGiven(input.TrackNumber, domain.ValidateTrackNumber); err != nil {
		return err
	}
	if err := whenGiven(input.DurationSeconds, func(seconds float64) error {
		if _, err := domain.TrackLengthOf(seconds); err != nil {
			return fmt.Errorf("track length of: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}
	if input.Year == nil {
		return nil
	}
	if err := domain.ValidateYear(*input.Year, now); err != nil {
		return fmt.Errorf("validate year: %w", err)
	}
	return nil
}

func validateAddTrackInput(input AddTrackInput, now time.Time) error {
	if err := validateAddTrackNumbers(input, now); err != nil {
		return err
	}
	if err := validateAddTrackText(input); err != nil {
		return err
	}
	if err := domain.ValidateFeaturedArtists(input.FeaturedArtists); err != nil {
		return fmt.Errorf("validate featured artists: %w", err)
	}
	if err := validateCatalogIdentity(input); err != nil {
		return err
	}
	if err := whenGiven(input.SourceURL, domain.ValidateSourceURL); err != nil {
		return err
	}
	return whenGiven(input.IdempotencyKey, domain.ValidateIdempotencyKey)
}

func validateCatalogIdentity(input AddTrackInput) error {
	if err := whenGiven(input.CatalogMBID, func(mbid string) error {
		if _, err := domain.NormalizeCatalogMBID(mbid); err != nil {
			return fmt.Errorf("normalize catalog mbid: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}
	if _, err := domain.NormalizeCatalogSources(input.CatalogSources); err != nil {
		return fmt.Errorf("normalize catalog sources: %w", err)
	}
	return nil
}

func validateAddTrackText(input AddTrackInput) error {
	fields := []struct {
		value *string
		name  string
	}{
		{input.ArtworkURL, "artwork_url"},
		{input.Genre, "genre"},
		{input.AlbumArtist, "album_artist"},
		{input.ISRC, "isrc"},
	}
	for _, f := range fields {
		if err := domain.ValidateOptionalTrackText(f.value, f.name); err != nil {
			return fmt.Errorf("validate optional track text: %w", err)
		}
	}
	return nil
}
