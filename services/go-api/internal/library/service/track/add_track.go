package track

import (
	"altune/go-api/internal/library/domain"
	"altune/go-api/internal/library/internal/core/ports"
	apierror "altune/go-api/internal/shared/apierror"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/ids"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"
)

const minPlausibleYear = 1860

const maxTrackNumber = 2147483647

const MaxFeaturedArtistsPerTrack = 100

const MaxTracksPerUser = 50_000

var ErrLibraryFull = apierror.NewError(apierror.CodeCatalogLibraryFull, "library is full")

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
	events    events.Publisher
	scheduler ports.AcquisitionScheduler
	now       func() time.Time
}

func NewAddTrackService(trackRepo ports.TrackAddUpdater, publisher events.Publisher, scheduler ports.AcquisitionScheduler, now func() time.Time) *AddTrackService {
	switch {
	case trackRepo == nil:
		panic("track: NewAddTrackService requires a track repository")
	case publisher == nil:
		panic("track: NewAddTrackService requires an events publisher")
	case scheduler == nil:
		panic("track: NewAddTrackService requires an acquisition scheduler")
	case now == nil:
		panic("track: NewAddTrackService requires a clock")
	}
	return &AddTrackService{trackRepo: trackRepo, events: publisher, scheduler: scheduler, now: now}
}

func (s *AddTrackService) Execute(ctx context.Context, userId ids.UserId, input AddTrackInput) (*AddTrackOutput, error) {
	if err := validateAddTrackInput(input, s.now()); err != nil {
		return nil, err
	}
	if err := s.requireLibrarySpace(ctx, userId); err != nil {
		return nil, err
	}
	track, err := domain.NewTrack(userId, input.Title, input.Artist, input.Album)
	if err != nil {
		return nil, err
	}
	if input.DurationSeconds != nil {
		if err := track.SetCatalogDuration(*input.DurationSeconds); err != nil {
			return nil, err
		}
	}
	sources := input.CatalogSources
	if input.SourceURL != nil {
		sources = domain.WithSoundCloudURL(sources, *input.SourceURL)
	}
	if err := track.SetCatalogIdentity(input.CatalogMBID, input.CatalogExplicit, sources); err != nil {
		return nil, err
	}
	track.ArtworkURL = input.ArtworkURL
	track.Year = input.Year
	track.Genre = input.Genre
	track.TrackNumber = input.TrackNumber
	if input.AlbumArtist != nil {
		track.SetAlbumArtist(*input.AlbumArtist)
	}
	track.ISRC = input.ISRC
	track.FeaturedArtists = input.FeaturedArtists
	track.IdempotencyKey = input.IdempotencyKey

	stored, created, err := s.trackRepo.Add(ctx, track)
	if err != nil {
		return nil, domain.WrapRepoError(ctx, "add track", err)
	}
	if stored != nil {
		track = stored
	}

	if created {
		slog.InfoContext(ctx, "track added to library",
			"track_id", track.ID.String(),
			"user_id", userId.String(),
		)
		if payload, encodeErr := trackAddedPayload(track); encodeErr != nil {
			slog.WarnContext(ctx, "catalog.track_added_event_dropped", "track_id", track.ID.String(), "error", encodeErr)
		} else {
			s.events.Publish(ctx, userId, events.TypeTrackAddedToLibrary, payload)
		}
		sourceURL := ""
		if input.SourceURL != nil {
			sourceURL = *input.SourceURL
		}
		s.scheduleAcquisition(ctx, userId, track, sourceURL)
	}

	return &AddTrackOutput{Track: track, Created: created}, nil
}

func (s *AddTrackService) requireLibrarySpace(ctx context.Context, userId ids.UserId) error {
	held, err := s.trackRepo.CountForUser(ctx, userId, MaxTracksPerUser)
	if err != nil {
		return domain.WrapRepoError(ctx, "count tracks", err)
	}
	if held >= MaxTracksPerUser {
		slog.WarnContext(ctx, "catalog.library_full", "user_id", userId.String(), "cap", MaxTracksPerUser)
		return ErrLibraryFull
	}
	return nil
}

const ScheduleTimeout = 5 * time.Second

func ScheduleBounded(ctx context.Context, scheduler ports.AcquisitionScheduler, userId ids.UserId, trackId domain.TrackId, sourceURL string) error {
	ctx, cancel := context.WithTimeout(ctx, ScheduleTimeout)
	defer cancel()
	return scheduler.Schedule(ctx, userId, trackId, sourceURL)
}

func (s *AddTrackService) scheduleAcquisition(ctx context.Context, userId ids.UserId, track *domain.Track, sourceURL string) {
	slog.InfoContext(ctx, "acquisition.scheduled", "track_id", track.ID.String())
	schedErr := ScheduleBounded(ctx, s.scheduler, userId, track.ID, sourceURL)
	if schedErr == nil {
		return
	}
	slog.WarnContext(ctx, "acquisition.schedule_refused",
		"track_id", track.ID.String(), "user_id", userId.String(), "error", schedErr)
	failed := *track
	expectedVersion := failed.Version
	_ = failed.MarkFailed(string(domain.FailureAcquisitionRefused))
	if err := s.trackRepo.Update(ctx, &failed, expectedVersion); err != nil {
		slog.ErrorContext(ctx, "acquisition.schedule_refused_persist_failed",
			"track_id", track.ID.String(), "error", err)
		return
	}
	*track = failed
	s.events.Publish(ctx, userId, events.TypeTrackAcquisitionFailed, map[string]any{
		"track_id": track.ID.String(),
		"reason":   string(domain.FailureAcquisitionRefused),
	})
}

func validateAddTrackInput(input AddTrackInput, now time.Time) error {
	if input.TrackNumber != nil && *input.TrackNumber <= 0 {
		return domain.NewValidationError("track_number must be positive")
	}
	if input.TrackNumber != nil && *input.TrackNumber > maxTrackNumber {
		return domain.NewValidationError("track_number exceeds maximum (int4)")
	}
	if input.DurationSeconds != nil {
		if _, err := domain.TrackLengthOf(*input.DurationSeconds); err != nil {
			return err
		}
	}
	if input.Year != nil && !plausibleYear(*input.Year, now) {
		return domain.NewValidationError("year is implausible")
	}
	if err := validateAddTrackText(input); err != nil {
		return err
	}
	if len(input.FeaturedArtists) > MaxFeaturedArtistsPerTrack {
		return domain.NewValidationError("featured_artists exceeds maximum count")
	}
	if err := domain.ValidateFeaturedArtists(input.FeaturedArtists); err != nil {
		return err
	}
	if err := validateCatalogIdentity(input); err != nil {
		return err
	}
	if input.SourceURL != nil {
		if err := domain.ValidateSourceURL(*input.SourceURL); err != nil {
			return err
		}
	}
	if err := validateIdempotencyKey(input.IdempotencyKey); err != nil {
		return err
	}
	return nil
}

func validateCatalogIdentity(input AddTrackInput) error {
	if input.CatalogMBID != nil {
		if _, err := domain.NormalizeCatalogMBID(*input.CatalogMBID); err != nil {
			return err
		}
	}
	_, err := domain.NormalizeCatalogSources(input.CatalogSources)
	return err
}

const maxIdempotencyKeyLength = 200

func validateIdempotencyKey(key *string) error {
	if key == nil {
		return nil
	}
	if *key == "" {
		return domain.NewValidationError("idempotency_key must not be empty")
	}
	if err := domain.ValidateText(*key, "idempotency_key"); err != nil {
		return err
	}
	if len(*key) > maxIdempotencyKeyLength {
		return domain.NewValidationError("idempotency_key exceeds maximum length")
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
			return err
		}
	}
	return nil
}

func plausibleYear(year int, now time.Time) bool {
	return year >= minPlausibleYear && year <= now.UTC().Year()+1
}

func trackAddedPayload(t *domain.Track) (map[string]any, error) {
	dto := TrackToDTO(t)
	raw, err := json.Marshal(dto)
	if err != nil {
		return nil, fmt.Errorf("encode track %s: %w", dto.ID, err)
	}
	payload := map[string]any{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("decode track %s payload: %w", dto.ID, err)
	}
	payload["track_id"] = dto.ID
	return payload, nil
}
