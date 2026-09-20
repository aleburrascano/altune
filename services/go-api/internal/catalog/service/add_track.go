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

// maxTrackNumber is the int4 ceiling of the track_number column (see
// migrations/001_baseline.sql). Values above it cannot encode into Postgres and
// would otherwise surface as an opaque 500 instead of a validation error.
const maxTrackNumber = 2147483647

// MaxFeaturedArtistsPerTrack caps featured_artists on a track add. Each entry
// costs two round trips inside the add transaction, so the list must not scale
// with caller input. Neither the mobile save payload nor discovery's
// MusicBrainz/Deezer extraction trims the list, so the cap sits well above real
// credits: the largest seen, a charity single like "We Are The World", carries
// 40 Deezer contributors, and merging MusicBrainz credits at most roughly
// doubles that.
const MaxFeaturedArtistsPerTrack = 100

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

// WithAddTrackClock replaces the clock the year plausibility ceiling is
// measured from. A nil clock is ignored so the wall clock always holds.
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

// scheduleTimeout bounds a single AcquisitionScheduler.Schedule call. Admission
// is an in-process queue check, so a call anywhere near this budget is stuck;
// the bound keeps it from holding the request goroutine indefinitely.
const scheduleTimeout = 5 * time.Second

// scheduleBounded calls scheduler.Schedule under a child context bounded by
// scheduleTimeout. Because it derives from ctx, a caller that already carries a
// shorter deadline keeps it. A timeout surfaces as a non-nil error, which
// callers treat like any other refused schedule.
func scheduleBounded(ctx context.Context, scheduler ports.AcquisitionScheduler, userId shared.UserId, trackId domain.TrackId, sourceURL string) error {
	ctx, cancel := context.WithTimeout(ctx, scheduleTimeout)
	defer cancel()
	return scheduler.Schedule(ctx, userId, trackId, sourceURL)
}

// scheduleAcquisition queues the new track's acquisition. When the scheduler
// refuses the job or times out, nothing will ever move the track off pending,
// so it is failed at once with a distinct reason: the retry path admits failed
// tracks. The track returned in AddTrackOutput reflects that degraded outcome.
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
	_ = failed.MarkFailed(domain.ReasonAcquisitionRefused)
	// CAS at the just-created row's version. A conflict here means an acquisition
	// writer already settled the track between Add and now, so its result stands
	// and this refusal write is dropped — logged, not swallowed, and the row is
	// reported as stored (pending); the stale-pending sweep still reclaims a
	// genuinely stuck pending row after its grace, keeping it retryable.
	if err := s.trackRepo.Update(ctx, &failed, expectedVersion); err != nil {
		slog.ErrorContext(ctx, "acquisition.schedule_refused_persist_failed",
			"track_id", track.ID.String(), "error", err)
		return
	}
	*track = failed
	s.events.Publish(ctx, userId, events.TypeTrackAcquisitionFailed, map[string]any{
		"track_id": track.ID.String(),
		"reason":   domain.ReasonAcquisitionRefused,
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

// maxIdempotencyKeyLength bounds the client-supplied key so a hostile client
// cannot store an unbounded token. A UUID is 36 chars; 200 leaves ample room
// for other reasonable key schemes.
const maxIdempotencyKeyLength = 200

func validateIdempotencyKey(key *string) error {
	if key == nil {
		return nil
	}
	if *key == "" {
		return domain.NewValidationError("idempotency_key must not be empty")
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
