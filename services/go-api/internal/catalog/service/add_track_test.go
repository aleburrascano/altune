package service

import (
	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/sharedtest"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func strptr(s string) *string { return &s }

// withTrackCap lowers the per-user library cap for one test, so crossing it
// costs a handful of rows rather than fifty thousand.
func withTrackCap(t *testing.T, limit int) {
	t.Helper()
	prev := maxTracksPerUser
	maxTracksPerUser = limit
	t.Cleanup(func() { maxTracksPerUser = prev })
}

// TestAddTrack_RejectsPastUserCap reproduces #2200: distinct titles bypass
// dedup, so nothing stopped one account from inserting tracks without limit.
// The save that would cross the cap is refused, and stores nothing.
func TestAddTrack_RejectsPastUserCap(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()
	repo := catalogtest.NewTrackRepo()
	withTrackCap(t, 3)
	for i := range maxTracksPerUser {
		seedTrack(t, repo, userId, fmt.Sprintf("Held %d", i), "Artist", "Album")
	}
	svc := NewAddTrackService(repo)

	out, err := svc.Execute(ctx, userId, AddTrackInput{Title: "One Too Many", Artist: "Artist", Album: "Album"})

	if !errors.Is(err, ErrLibraryFull) {
		t.Fatalf("error = %v, want ErrLibraryFull", err)
	}
	if out != nil {
		t.Fatalf("output = %+v, want nil", out)
	}
	if len(repo.Tracks) != maxTracksPerUser {
		t.Fatalf("stored tracks = %d, want %d: the refused save must not insert", len(repo.Tracks), maxTracksPerUser)
	}
}

// The cap counts the caller's own rows: a library full next door may not
// refuse this account's save.
func TestAddTrack_CapCountsOnlyTheCallersTracks(t *testing.T) {
	ctx := context.Background()
	repo := catalogtest.NewTrackRepo()
	withTrackCap(t, 2)
	for i := range maxTracksPerUser {
		seedTrack(t, repo, testOtherUserId(), fmt.Sprintf("Theirs %d", i), "Artist", "Album")
	}
	svc := NewAddTrackService(repo)

	out, err := svc.Execute(ctx, testUserId(), AddTrackInput{Title: "Mine", Artist: "Artist", Album: "Album"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !out.Created {
		t.Fatal("Created = false, want true: another owner's rows are not this caller's cap")
	}
}

// A second save carrying the same idempotency key must return the first stored
// track (created=false), even when its content differs — the key, not the
// content, decides identity here.
func TestAddTrackService_IdempotencyKeyReturnsExisting(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()
	repo := catalogtest.NewTrackRepo()
	svc := NewAddTrackService(repo)
	key := "save-abc-123"

	first, err := svc.Execute(ctx, userId, AddTrackInput{
		Title: "One", Artist: "Artist", Album: "Album", IdempotencyKey: strptr(key),
	})
	if err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	if !first.Created {
		t.Fatalf("first Created = false, want true")
	}

	second, err := svc.Execute(ctx, userId, AddTrackInput{
		Title: "Different Title", Artist: "Other", Album: "Other", IdempotencyKey: strptr(key),
	})
	if err != nil {
		t.Fatalf("second Execute: %v", err)
	}
	if second.Created {
		t.Fatalf("second Created = true, want false (same key must collapse)")
	}
	if second.Track.ID != first.Track.ID {
		t.Fatalf("second track id = %s, want first %s", second.Track.ID, first.Track.ID)
	}
	if len(repo.Tracks) != 1 {
		t.Fatalf("stored tracks = %d, want 1", len(repo.Tracks))
	}
}

func TestAddTrackService_RejectsEmptyIdempotencyKey(t *testing.T) {
	ctx := context.Background()
	repo := catalogtest.NewTrackRepo()
	svc := NewAddTrackService(repo)

	_, err := svc.Execute(ctx, testUserId(), AddTrackInput{
		Title: "T", Artist: "A", Album: "Al", IdempotencyKey: strptr(""),
	})
	if err == nil || !strings.Contains(err.Error(), "idempotency_key must not be empty") {
		t.Fatalf("err = %v, want empty idempotency_key validation error", err)
	}
}

func TestAddTrackService_Execute(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()
	errRepo := errors.New("db connection lost")

	tests := []struct {
		name        string
		input       AddTrackInput
		setup       func(*catalogtest.TrackRepo)
		wantCreated bool
		wantTitle   string
		wantErr     string
	}{
		{
			name: "new track is created",
			input: AddTrackInput{
				Title:  "Track",
				Artist: "Artist",
				Album:  "Album",
			},
			wantCreated: true,
			wantTitle:   "Track",
		},
		{
			name: "duplicate returns existing track not created",
			input: AddTrackInput{
				Title:  "Existing",
				Artist: "Artist",
				Album:  "Album",
			},
			setup: func(repo *catalogtest.TrackRepo) {
				seedTrack(t, repo, userId, "Existing", "Artist", "Album")
			},
			wantCreated: false,
			wantTitle:   "Existing",
		},
		{
			name: "empty title returns validation error",
			input: AddTrackInput{
				Title:  "",
				Artist: "Artist",
				Album:  "Album",
			},
			wantErr: "track title required",
		},
		{
			name: "empty artist returns validation error",
			input: AddTrackInput{
				Title:  "Track",
				Artist: "",
				Album:  "Album",
			},
			wantErr: "track artist required",
		},
		{
			name: "repo error propagates",
			input: AddTrackInput{
				Title:  "Track",
				Artist: "Artist",
				Album:  "Album",
			},
			setup: func(repo *catalogtest.TrackRepo) {
				repo.ErrOnAdd = errRepo
			},
			wantErr: "db connection lost",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := catalogtest.NewTrackRepo()
			if tt.setup != nil {
				tt.setup(repo)
			}
			svc := NewAddTrackService(repo)

			out, err := svc.Execute(ctx, userId, tt.input)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %q, want it to contain %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if out.Created != tt.wantCreated {
				t.Errorf("Created = %v, want %v", out.Created, tt.wantCreated)
			}
			if out.Track == nil {
				t.Fatal("expected non-nil Track in output")
			}
			if out.Track.Title != tt.wantTitle {
				t.Errorf("Track.Title = %q, want %q", out.Track.Title, tt.wantTitle)
			}
		})
	}
}

func TestTrackAddedPayload(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	track, err := domain.NewTrack(userId, "Midnight City", "M83", "Hurry Up, We're Dreaming")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}

	p := trackAddedPayload(track)

	if p["id"] != track.ID.String() {
		t.Errorf("id = %v, want %s", p["id"], track.ID.String())
	}
	if p["track_id"] != track.ID.String() {
		t.Errorf("track_id (legacy) = %v, want %s", p["track_id"], track.ID.String())
	}
	if p["title"] != "Midnight City" || p["artist"] != "M83" {
		t.Errorf("title/artist = %v/%v", p["title"], p["artist"])
	}
	if album, ok := p["album"].(string); !ok || album != "Hurry Up, We're Dreaming" {
		t.Errorf("album = %v, want the album string", p["album"])
	}
	if p["acquisition_status"] != track.AcquisitionStatus.String() {
		t.Errorf("acquisition_status = %v", p["acquisition_status"])
	}
}

func TestTrackAddedPayload_EmptyAlbumIsNil(t *testing.T) {
	track := &domain.Track{
		ID:     domain.NewTrackId(),
		UserId: shared.NewUserId(uuid.New()),
		Title:  "Single",
		Artist: "Artist",
		Album:  "",
	}

	if album := trackAddedPayload(track)["album"]; album != nil {
		t.Errorf("empty album = %v, want JSON null", album)
	}
}

func TestTrackAddedPayload_SingleCarriesTheTitleAsAlbum(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	track, err := domain.NewTrack(userId, "Single", "Artist", "")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}

	if album := trackAddedPayload(track)["album"]; album != "Single" {
		t.Errorf("album = %v, want the title", album)
	}
}

func TestTrackAddedPayload_MatchesRESTDTOByteForByte(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	track, err := domain.NewTrack(userId, "Midnight City", "M83", "Hurry Up, We're Dreaming")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	dur := 240.5
	artwork := "https://cdn.example/art.jpg"
	year := 2011
	genre := "electronic"
	trackNo := 4
	albumArtist := "M83"
	isrc := "USUM71100001"
	audioRef := "obj/abc123"
	reason := "no_source"
	track.DurationSeconds = &dur
	track.ArtworkURL = &artwork
	track.Year = &year
	track.Genre = &genre
	track.TrackNumber = &trackNo
	track.AlbumArtist = &albumArtist
	track.ISRC = &isrc
	track.AudioRef = &audioRef
	track.FailureReason = &reason
	track.AcquisitionStatus = domain.AcquisitionFailed
	track.FeaturedArtists = []domain.FeaturedArtist{
		domain.NewFeaturedArtistIdentityOnly("Susanne Sundfor", "11111111-2222-3333-4444-555555555555", 4567),
	}

	restJSON := canonicalJSON(t, mustMarshal(t, TrackToDTO(track)))

	payload := trackAddedPayload(track)
	if payload["track_id"] != track.ID.String() {
		t.Errorf("track_id = %v, want %s", payload["track_id"], track.ID.String())
	}
	delete(payload, "track_id")
	sseJSON := canonicalJSON(t, mustMarshal(t, payload))

	if restJSON != sseJSON {
		t.Errorf("SSE payload diverged from REST DTO\n REST: %s\n  SSE: %s", restJSON, sseJSON)
	}
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return raw
}

func canonicalJSON(t *testing.T, raw []byte) string {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(out)
}

// TestAddTrackService_RefusedScheduleFailsTrack is the regression for a shed
// acquisition stranding a new track at pending: nothing would ever run its job,
// and the retry endpoint only admits failed tracks. A refused schedule must fail
// the track at once, persist it, and tell the client, so retry can reclaim it.
func TestAddTrackService_RefusedScheduleFailsTrack(t *testing.T) {
	ctx := context.Background()
	repo := catalogtest.NewTrackRepo()
	sched := &catalogtest.Scheduler{Err: errors.New("acquisition queue is full")}
	pub := &recordingPlaylistPublisher{}
	svc := NewAddTrackService(repo, WithAcquisitionScheduler(sched), WithAddTrackEvents(pub))

	out, err := svc.Execute(ctx, testUserId(), AddTrackInput{Title: "Track", Artist: "Artist", Album: "Album"})
	if err != nil {
		t.Fatalf("Execute = %v, want the track created despite the refused schedule", err)
	}
	if len(sched.TrackIds) != 1 {
		t.Fatalf("schedule attempts = %d, want 1", len(sched.TrackIds))
	}

	stored, _ := repo.GetByID(ctx, out.Track.ID, out.Track.UserId)
	if stored == nil || stored.AcquisitionStatus != domain.AcquisitionFailed {
		t.Fatalf("stored track = %+v, want failed (not stranded pending)", stored)
	}
	if stored.FailureReason == nil || *stored.FailureReason != string(domain.FailureAcquisitionRefused) {
		t.Errorf("failure reason = %v, want %q", stored.FailureReason, domain.FailureAcquisitionRefused)
	}
	if stored.AcquisitionStartedAt != nil {
		t.Error("in-flight marker left set on a track whose job was never queued")
	}
	if got := pub.last("track_acquisition_failed"); got == nil || got["reason"] != string(domain.FailureAcquisitionRefused) {
		t.Errorf("track_acquisition_failed payload = %v, want reason %q", got, domain.FailureAcquisitionRefused)
	}
}

// If the failed state cannot be persisted, the response and events must not
// claim it: the stored row is still pending (the stale sweep reclaims it).
func TestAddTrackService_RefusedScheduleUnpersistedStaysPending(t *testing.T) {
	ctx := context.Background()
	repo := catalogtest.NewTrackRepo()
	repo.ErrOnUpdate = errors.New("db down")
	pub := &recordingPlaylistPublisher{}
	sched := &catalogtest.Scheduler{Err: errors.New("acquisition queue is full")}
	svc := NewAddTrackService(repo, WithAcquisitionScheduler(sched), WithAddTrackEvents(pub))

	out, err := svc.Execute(ctx, testUserId(), AddTrackInput{Title: "Track", Artist: "Artist", Album: "Album"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Track.AcquisitionStatus != domain.AcquisitionPending || out.Track.AcquisitionStartedAt == nil {
		t.Errorf("returned track = %v (started_at %v), want pending as stored", out.Track.AcquisitionStatus, out.Track.AcquisitionStartedAt)
	}
	if pub.last("track_acquisition_failed") != nil {
		t.Error("published track_acquisition_failed for a failure that was never persisted")
	}
}

func TestAddTrackService_AcceptedScheduleLeavesTrackPending(t *testing.T) {
	ctx := context.Background()
	repo := catalogtest.NewTrackRepo()
	pub := &recordingPlaylistPublisher{}
	svc := NewAddTrackService(repo, WithAcquisitionScheduler(&catalogtest.Scheduler{}), WithAddTrackEvents(pub))

	out, err := svc.Execute(ctx, testUserId(), AddTrackInput{Title: "Track", Artist: "Artist", Album: "Album"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Track.AcquisitionStatus != domain.AcquisitionPending {
		t.Errorf("status = %v, want pending while the queued job runs", out.Track.AcquisitionStatus)
	}
	if pub.last("track_acquisition_failed") != nil {
		t.Error("published track_acquisition_failed for an accepted schedule")
	}
}

func TestAddTrackService_ValidatesRanges(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()

	negDuration := -100.0
	posInfDuration := math.Inf(1)
	nanDuration := math.NaN()
	hugeDuration := math.MaxFloat64
	zeroTrackNumber := 0
	negTrackNumber := -3
	hugeTrackNumber := 3000000000
	farFutureYear := time.Now().UTC().Year() + 50
	ancientYear := 1000

	tests := []struct {
		name    string
		mutate  func(*AddTrackInput)
		wantErr string
	}{
		{
			name:    "negative duration is rejected",
			mutate:  func(in *AddTrackInput) { in.DurationSeconds = &negDuration },
			wantErr: "duration_seconds",
		},
		{
			name:    "+Inf duration is rejected",
			mutate:  func(in *AddTrackInput) { in.DurationSeconds = &posInfDuration },
			wantErr: "duration_seconds",
		},
		{
			name:    "NaN duration is rejected",
			mutate:  func(in *AddTrackInput) { in.DurationSeconds = &nanDuration },
			wantErr: "duration_seconds",
		},
		{
			name:    "duration above the cap is rejected",
			mutate:  func(in *AddTrackInput) { in.DurationSeconds = &hugeDuration },
			wantErr: "duration_seconds",
		},
		{
			name:    "zero track number is rejected",
			mutate:  func(in *AddTrackInput) { in.TrackNumber = &zeroTrackNumber },
			wantErr: "track_number",
		},
		{
			name:    "negative track number is rejected",
			mutate:  func(in *AddTrackInput) { in.TrackNumber = &negTrackNumber },
			wantErr: "track_number",
		},
		{
			name:    "out-of-range track number is rejected",
			mutate:  func(in *AddTrackInput) { in.TrackNumber = &hugeTrackNumber },
			wantErr: "track_number",
		},
		{
			name:    "far-future year is rejected",
			mutate:  func(in *AddTrackInput) { in.Year = &farFutureYear },
			wantErr: "year",
		},
		{
			name:    "ancient year is rejected",
			mutate:  func(in *AddTrackInput) { in.Year = &ancientYear },
			wantErr: "year",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := catalogtest.NewTrackRepo()
			svc := NewAddTrackService(repo)
			input := AddTrackInput{Title: "Track", Artist: "Artist", Album: "Album"}
			tt.mutate(&input)

			out, err := svc.Execute(ctx, userId, input)

			if err == nil {
				t.Fatalf("expected a validation error, got nil (out=%+v)", out)
			}
			sharedtest.AssertValidationError(t, err)
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %q, want it to mention %q", err.Error(), tt.wantErr)
			}
			if len(repo.Tracks) != 0 {
				t.Fatalf("stored %d tracks, want the invalid track rejected before persistence", len(repo.Tracks))
			}
		})
	}
}

// TestAddTrackService_YearCeilingSitsOneYearPastTheClock pins the ceiling
// against an injected clock: next year is a legitimate pre-release date, the
// year after it is not.
func TestAddTrackService_YearCeilingSitsOneYearPastTheClock(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()
	pinned := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		year       int
		wantStored int
	}{
		{name: "the year after the clock's is accepted", year: 2027, wantStored: 1},
		{name: "two years after the clock's is rejected", year: 2028, wantStored: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := catalogtest.NewTrackRepo()
			svc := NewAddTrackService(repo, WithAddTrackClock(func() time.Time { return pinned }))
			input := AddTrackInput{Title: "Track", Artist: "Artist", Album: "Album", Year: &tt.year}

			_, err := svc.Execute(ctx, userId, input)

			if tt.wantStored == 0 {
				sharedtest.AssertValidationError(t, err)
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(repo.Tracks) != tt.wantStored {
				t.Fatalf("stored %d tracks, want %d for year %d", len(repo.Tracks), tt.wantStored, tt.year)
			}
		})
	}
}

func TestAddTrackService_ValidatesFreeFormFields(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()

	oversized := strings.Repeat("x", 301)
	malformedURL := "not a url"
	schemelessURL := "example.com/song.mp3"
	ftpURL := "ftp://example.com/song.mp3"
	metadataURL := "http://169.254.169.254/latest/meta-data/"
	loopbackURL := "http://127.0.0.1/admin"

	tests := []struct {
		name    string
		mutate  func(*AddTrackInput)
		wantErr string
	}{
		{
			name:    "oversized artwork_url is rejected",
			mutate:  func(in *AddTrackInput) { in.ArtworkURL = &oversized },
			wantErr: "artwork_url",
		},
		{
			name:    "oversized genre is rejected",
			mutate:  func(in *AddTrackInput) { in.Genre = &oversized },
			wantErr: "genre",
		},
		{
			name:    "oversized album_artist is rejected",
			mutate:  func(in *AddTrackInput) { in.AlbumArtist = &oversized },
			wantErr: "album_artist",
		},
		{
			name:    "oversized isrc is rejected",
			mutate:  func(in *AddTrackInput) { in.ISRC = &oversized },
			wantErr: "isrc",
		},
		{
			name:    "malformed source_url is rejected",
			mutate:  func(in *AddTrackInput) { in.SourceURL = &malformedURL },
			wantErr: "source_url",
		},
		{
			name:    "schemeless source_url is rejected",
			mutate:  func(in *AddTrackInput) { in.SourceURL = &schemelessURL },
			wantErr: "source_url",
		},
		{
			name:    "non-http source_url is rejected",
			mutate:  func(in *AddTrackInput) { in.SourceURL = &ftpURL },
			wantErr: "source_url",
		},
		{
			name:    "metadata-endpoint source_url is rejected",
			mutate:  func(in *AddTrackInput) { in.SourceURL = &metadataURL },
			wantErr: "source_url must target a public host",
		},
		{
			name:    "loopback source_url is rejected",
			mutate:  func(in *AddTrackInput) { in.SourceURL = &loopbackURL },
			wantErr: "source_url must target a public host",
		},
		{
			name:    "oversized source_url is rejected",
			mutate:  func(in *AddTrackInput) { in.SourceURL = &oversized },
			wantErr: "source_url",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := catalogtest.NewTrackRepo()
			svc := NewAddTrackService(repo)
			input := AddTrackInput{Title: "Track", Artist: "Artist", Album: "Album"}
			tt.mutate(&input)

			out, err := svc.Execute(ctx, userId, input)

			if err == nil {
				t.Fatalf("expected a validation error, got nil (out=%+v)", out)
			}
			sharedtest.AssertValidationError(t, err)
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %q, want it to mention %q", err.Error(), tt.wantErr)
			}
			if len(repo.Tracks) != 0 {
				t.Fatalf("stored %d tracks, want the invalid track rejected before persistence", len(repo.Tracks))
			}
		})
	}
}

func TestAddTrackService_AcceptsValidFreeFormFields(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()

	artwork := "https://cdn.example.com/art.jpg"
	genre := "Jazz"
	albumArtist := "Various Artists"
	isrc := "USUM71703861"
	sourceURL := "https://example.com/song.mp3"

	repo := catalogtest.NewTrackRepo()
	svc := NewAddTrackService(repo)
	input := AddTrackInput{
		Title:       "Track",
		Artist:      "Artist",
		Album:       "Album",
		ArtworkURL:  &artwork,
		Genre:       &genre,
		AlbumArtist: &albumArtist,
		ISRC:        &isrc,
		SourceURL:   &sourceURL,
	}

	out, err := svc.Execute(ctx, userId, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Track.ArtworkURL == nil || *out.Track.ArtworkURL != artwork {
		t.Errorf("ArtworkURL = %v, want %v", out.Track.ArtworkURL, artwork)
	}
	if out.Track.ISRC == nil || *out.Track.ISRC != isrc {
		t.Errorf("ISRC = %v, want %v", out.Track.ISRC, isrc)
	}
}

func TestAddTrackService_AcceptsValidRanges(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()

	duration := 210.5
	trackNumber := 7
	year := time.Now().UTC().Year()

	repo := catalogtest.NewTrackRepo()
	svc := NewAddTrackService(repo)
	input := AddTrackInput{
		Title:           "Track",
		Artist:          "Artist",
		Album:           "Album",
		DurationSeconds: &duration,
		TrackNumber:     &trackNumber,
		Year:            &year,
	}

	out, err := svc.Execute(ctx, userId, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Track.DurationSeconds == nil || *out.Track.DurationSeconds != duration {
		t.Errorf("DurationSeconds = %v, want %v", out.Track.DurationSeconds, duration)
	}
	if out.Track.TrackNumber == nil || *out.Track.TrackNumber != trackNumber {
		t.Errorf("TrackNumber = %v, want %v", out.Track.TrackNumber, trackNumber)
	}
	if out.Track.Year == nil || *out.Track.Year != year {
		t.Errorf("Year = %v, want %v", out.Track.Year, year)
	}
}

// Regression for #1055: a request with no deadline of its own must still hand
// Schedule a bounded context, or a stuck call holds the handler goroutine forever.
func TestAddTrackService_ScheduleBoundedByTimeout(t *testing.T) {
	sched := &stuckScheduler{}
	svc := NewAddTrackService(catalogtest.NewTrackRepo(), WithAcquisitionScheduler(sched))

	start := time.Now()
	ctx, cancel := context.WithCancel(context.Background())
	// Release the stuck call once its deadline has been observed so the test
	// does not wait the full production timeout.
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	if _, err := svc.Execute(ctx, testUserId(), AddTrackInput{Title: "T", Artist: "A", Album: "B"}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	assertScheduleDeadline(t, sched, start)
}

// A caller that already carries a shorter budget keeps it, and a Schedule that
// runs out of it degrades the added track to failed rather than reporting success.
func TestAddTrackService_ScheduleTimeoutFailsTrack(t *testing.T) {
	repo := catalogtest.NewTrackRepo()
	svc := NewAddTrackService(repo, WithAcquisitionScheduler(&stuckScheduler{}))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	out, err := svc.Execute(ctx, testUserId(), AddTrackInput{Title: "T", Artist: "A", Album: "B"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Track.AcquisitionStatus != domain.AcquisitionFailed {
		t.Errorf("status = %v, want failed after a timed-out schedule", out.Track.AcquisitionStatus)
	}
	if out.Track.FailureReason == nil || *out.Track.FailureReason != string(domain.FailureAcquisitionRefused) {
		t.Errorf("failure reason = %v, want %q", out.Track.FailureReason, domain.FailureAcquisitionRefused)
	}
}
