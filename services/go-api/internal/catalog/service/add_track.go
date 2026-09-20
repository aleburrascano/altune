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

// MaxTracksPerUser is the most tracks one account may create. Distinct titles
// bypass dedup, so without it an authenticated caller grows the tracks table
// and its four trigram GIN indexes without limit (#2200). It sits an order of
// magnitude above the largest personal library anyone has brought here (the
// catalog pages every read, so nothing but storage bounds a real one).
const MaxTracksPerUser = 50_000

// maxTracksPerUser is the cap the check actually reads. It is a var so a test
// can cross it with a handful of rows instead of fifty thousand.
var maxTracksPerUser = MaxTracksPerUser

// ErrLibraryFull refuses a create once the account already holds
// MaxTracksPerUser tracks. A library stored over the cap keeps every track it
// has; only further creates are refused.
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

// requireLibrarySpace refuses the save once the account holds maxTracksPerUser
// tracks. The count is read before the insert, so saves racing the last slot
// can all pass it: the cap bounds growth, it is not an exact quota, and the
// per-user write throttle in front of the route bounds the overshoot to the
// requests one caller has in flight. At the cap every save is refused,
// including a retry of one that already landed, which would otherwise have
// answered with the stored track.
func (s *AddTrackService) requireLibrarySpace(ctx context.Context, userId shared.UserId) error {
	held, err := s.trackRepo.CountForUser(ctx, userId, maxTracksPerUser)
	if err != nil {
		return wrapRepoError(ctx, "count tracks", err)
	}
	if held >= maxTracksPerUser {
		// The refusal is a coded 400, which the HTTP layer does not log, and an
		// account that has stopped being able to save is worth seeing without a
		// client report.
		slog.WarnContext(ctx, "catalog.library_full", "user_id", userId.String(), "cap", maxTracksPerUser)
		return ErrLibraryFull
	}
	return nil
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
	_ = failed.MarkFailed(string(domain.FailureAcquisitionRefused))
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
