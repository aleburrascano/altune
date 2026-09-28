package service

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/events"
	"context"
	"encoding/json"
	"log/slog"
	"time"
)

const minPlausibleYear = 1860

const maxTrackNumber = 2147483647

const MaxFeaturedArtistsPerTrack = 100

const MaxTracksPerUser = 50_000

var maxTracksPerUser = MaxTracksPerUser

var ErrLibraryFull = &domain.CodedError{
	Msg:    "library is full",
	Status: 400,
	Code:   "catalog.library_full",
}

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

func NewAddTrackService(trackRepo ports.TrackAddUpdater, opts ...func(*AddTrackService)) *AddTrackService {
	s := &AddTrackService{
		trackRepo: trackRepo,
		events:    events.NoopPublisher(),
		scheduler: ports.NoopAcquisitionScheduler(),
		now:       time.Now,
	}
	return applyOptions(s, opts)
}

func WithAddTrackClock(now func() time.Time) func(*AddTrackService) {
	return func(s *AddTrackService) {
		if now != nil {
			s.now = now
		}
	}
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
		if err := track.SetDuration(*input.DurationSeconds); err != nil {
			return nil, err
		}
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
		s.events.Publish(ctx, userId, events.TypeTrackAddedToLibrary, trackAddedPayload(track))
		sourceURL := ""
		if input.SourceURL != nil {
			sourceURL = *input.SourceURL
		}
		s.scheduleAcquisition(ctx, userId, track, sourceURL)
	}

	return &AddTrackOutput{Track: track, Created: created}, nil
}

func (s *AddTrackService) requireLibrarySpace(ctx context.Context, userId shared.UserId) error {
	held, err := s.trackRepo.CountForUser(ctx, userId, maxTracksPerUser)
	if err != nil {
		return wrapRepoError(ctx, "count tracks", err)
	}
	if held >= maxTracksPerUser {
		slog.WarnContext(ctx, "catalog.library_full", "user_id", userId.String(), "cap", maxTracksPerUser)
		return ErrLibraryFull
	}
	return nil
}

const scheduleTimeout = 5 * time.Second

func scheduleBounded(ctx context.Context, scheduler ports.AcquisitionScheduler, userId shared.UserId, trackId domain.TrackId, sourceURL string) error {
	ctx, cancel := context.WithTimeout(ctx, scheduleTimeout)
	defer cancel()
	return scheduler.Schedule(ctx, userId, trackId, sourceURL)
}

func (s *AddTrackService) scheduleAcquisition(ctx context.Context, userId shared.UserId, track *domain.Track, sourceURL string) {
	slog.InfoContext(ctx, "acquisition.scheduled", "track_id", track.ID.String())
	schedErr := scheduleBounded(ctx, s.scheduler, userId, track.ID, sourceURL)
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
		if err := domain.ValidateDurationSeconds(*input.DurationSeconds); err != nil {
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

func trackAddedPayload(t *domain.Track) map[string]any {
	dto := TrackToDTO(t)
	raw, _ := json.Marshal(dto)
	payload := map[string]any{}
	_ = json.Unmarshal(raw, &payload)
	payload["track_id"] = dto.ID.String()
	return payload
}
