package track_test

import (
	"altune/go-api/internal/library"
	"altune/go-api/internal/library/librarytest"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/dbtest"
	"altune/go-api/internal/shared/downloadfailure"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/ids"
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

const (
	addTrackScheduleBound = 5 * time.Second
	smallLibraryCap       = 2
)

func strptr(s string) *string { return &s }

func capInput(title string, key *string) library.AddTrackInput {
	return library.AddTrackInput{Title: title, Artist: "Artist", Album: "Album", IdempotencyKey: key}
}

func newCappedService() (*library.AddTrackService, *library.InMemoryTrackRepo) {
	repo := library.NewInMemoryTrackRepo(library.WithInMemoryLibraryCap(smallLibraryCap))
	return library.MustNewAddTrackService(repo, events.NoopPublisher(), &library.InMemoryScheduler{}, time.Now, ids.Real), repo
}

func TestAddTrack_RejectsPastUserCap(t *testing.T) {
	userId := librarytest.UserId()
	svc, repo := newCappedService()
	for i := range smallLibraryCap {
		if _, err := svc.Execute(context.Background(), userId, capInput(fmt.Sprintf("Track %d", i), nil)); err != nil {
			t.Fatalf("save %d: %v", i, err)
		}
	}

	out, err := svc.Execute(context.Background(), userId, capInput("One Too Many", nil))

	if !errors.Is(err, library.ErrLibraryFull) {
		t.Fatalf("error = %v, want ErrLibraryFull", err)
	}
	if out != nil {
		t.Fatalf("output = %+v, want nil", out)
	}
	if len(repo.Tracks) != smallLibraryCap {
		t.Fatalf("stored tracks = %d, want %d: the refused save must not insert", len(repo.Tracks), smallLibraryCap)
	}
}

func TestAddTrack_CapCountsOnlyTheCallersTracks(t *testing.T) {
	svc, _ := newCappedService()
	for i := range smallLibraryCap {
		if _, err := svc.Execute(context.Background(), librarytest.OtherUserId(), capInput(fmt.Sprintf("Theirs %d", i), nil)); err != nil {
			t.Fatalf("other owner save %d: %v", i, err)
		}
	}

	out, err := svc.Execute(context.Background(), librarytest.UserId(), capInput("Mine", nil))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !out.Created {
		t.Fatal("Created = false, want true: another owner's full library is not this caller's cap")
	}
}

func TestAddTrack_ReplayOfAKeyedSaveAtTheCapReturnsTheStoredTrack(t *testing.T) {
	userId := librarytest.UserId()
	svc, repo := newCappedService()
	first, err := svc.Execute(context.Background(), userId, capInput("Keyed", strptr("save-at-cap")))
	if err != nil {
		t.Fatalf("first save: %v", err)
	}
	if _, err := svc.Execute(context.Background(), userId, capInput("Filler", nil)); err != nil {
		t.Fatalf("filler save: %v", err)
	}

	replay, err := svc.Execute(context.Background(), userId, capInput("Keyed", strptr("save-at-cap")))
	if err != nil {
		t.Fatalf("replay at the cap: %v, want the stored track", err)
	}
	if replay.Created || replay.Track.ID() != first.Track.ID() {
		t.Fatalf("replay = created %v id %s, want created false id %s", replay.Created, replay.Track.ID(), first.Track.ID())
	}
	if len(repo.Tracks) != smallLibraryCap {
		t.Fatalf("stored tracks = %d, want %d", len(repo.Tracks), smallLibraryCap)
	}
}

func TestAddTrackService_IdempotencyKeyReturnsExisting(t *testing.T) {
	ctx := context.Background()
	userId := librarytest.UserId()
	repo := library.NewInMemoryTrackRepo()
	svc := library.MustNewAddTrackService(repo, events.NoopPublisher(), &library.InMemoryScheduler{}, time.Now, ids.Real)
	key := "save-abc-123"

	first, err := svc.Execute(ctx, userId, library.AddTrackInput{
		Title: "One", Artist: "Artist", Album: "Album", IdempotencyKey: strptr(key),
	})
	if err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	if !first.Created {
		t.Fatalf("first Created = false, want true")
	}

	second, err := svc.Execute(ctx, userId, library.AddTrackInput{
		Title: "Different Title", Artist: "Other", Album: "Other", IdempotencyKey: strptr(key),
	})
	if err != nil {
		t.Fatalf("second Execute: %v", err)
	}
	if second.Created {
		t.Fatalf("second Created = true, want false (same key must collapse)")
	}
	if second.Track.ID() != first.Track.ID() {
		t.Fatalf("second track id = %s, want first %s", second.Track.ID(), first.Track.ID())
	}
	if len(repo.Tracks) != 1 {
		t.Fatalf("stored tracks = %d, want 1", len(repo.Tracks))
	}
}

func TestAddTrackService_RejectsEmptyIdempotencyKey(t *testing.T) {
	ctx := context.Background()
	repo := library.NewInMemoryTrackRepo()
	svc := library.MustNewAddTrackService(repo, events.NoopPublisher(), &library.InMemoryScheduler{}, time.Now, ids.Real)

	_, err := svc.Execute(ctx, librarytest.UserId(), library.AddTrackInput{
		Title: "T", Artist: "A", Album: "Al", IdempotencyKey: strptr(""),
	})
	if err == nil || !strings.Contains(err.Error(), "idempotency_key must not be empty") {
		t.Fatalf("err = %v, want empty idempotency_key validation error", err)
	}
}

func TestAddTrackService_Execute(t *testing.T) {
	ctx := context.Background()
	userId := librarytest.UserId()
	errRepo := errors.New("db connection lost")

	tests := []struct {
		name        string
		input       library.AddTrackInput
		setup       func(*library.InMemoryTrackRepo)
		wantCreated bool
		wantTitle   string
		wantErr     string
	}{
		{
			name:        "new track is created",
			input:       library.AddTrackInput{Title: "Track", Artist: "Artist", Album: "Album"},
			wantCreated: true,
			wantTitle:   "Track",
		},
		{
			name:  "duplicate returns existing track not created",
			input: library.AddTrackInput{Title: "Existing", Artist: "Artist", Album: "Album"},
			setup: func(repo *library.InMemoryTrackRepo) {
				librarytest.SeedTrack(t, repo, userId, "Existing", "Artist", "Album")
			},
			wantCreated: false,
			wantTitle:   "Existing",
		},
		{
			name:    "empty title returns validation error",
			input:   library.AddTrackInput{Title: "", Artist: "Artist", Album: "Album"},
			wantErr: "track title required",
		},
		{
			name:    "empty artist returns validation error",
			input:   library.AddTrackInput{Title: "Track", Artist: "", Album: "Album"},
			wantErr: "track artist required",
		},
		{
			name:  "repo error propagates",
			input: library.AddTrackInput{Title: "Track", Artist: "Artist", Album: "Album"},
			setup: func(repo *library.InMemoryTrackRepo) {
				repo.ErrOnAdd = errRepo
			},
			wantErr: "db connection lost",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := library.NewInMemoryTrackRepo()
			if tt.setup != nil {
				tt.setup(repo)
			}
			svc := library.MustNewAddTrackService(repo, events.NoopPublisher(), &library.InMemoryScheduler{}, time.Now, ids.Real)

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
			if out.Track.Title() != tt.wantTitle {
				t.Errorf("Track.Title = %q, want %q", out.Track.Title(), tt.wantTitle)
			}
		})
	}
}

func addWithRecordedEvents(t *testing.T, input library.AddTrackInput) (*library.Track, map[string]any) {
	t.Helper()
	pub := &library.InMemoryRecordingPublisher{}
	svc := library.MustNewAddTrackService(library.NewInMemoryTrackRepo(), pub, &library.InMemoryScheduler{}, time.Now, ids.Real)
	out, err := svc.Execute(context.Background(), librarytest.UserId(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	payload := pub.LastFields(events.TypeTrackAddedToLibrary)
	if payload == nil {
		t.Fatal("no track_added_to_library event published")
	}
	return out.Track, payload
}

func TestTrackAddedPayload(t *testing.T) {
	track, p := addWithRecordedEvents(t, library.AddTrackInput{
		Title: "Midnight City", Artist: "M83", Album: "Hurry Up, We're Dreaming",
	})

	if p["id"] != track.ID().String() {
		t.Errorf("id = %v, want %s", p["id"], track.ID().String())
	}
	if p["track_id"] != track.ID().String() {
		t.Errorf("track_id (legacy) = %v, want %s", p["track_id"], track.ID().String())
	}
	if p["title"] != "Midnight City" || p["artist"] != "M83" {
		t.Errorf("title/artist = %v/%v", p["title"], p["artist"])
	}
	if album, ok := p["album"].(string); !ok || album != "Hurry Up, We're Dreaming" {
		t.Errorf("album = %v, want the album string", p["album"])
	}
	if p["acquisition_status"] != track.DownloadStatus().String() {
		t.Errorf("acquisition_status = %v", p["acquisition_status"])
	}
	if _, ok := p["audio_ref"]; ok {
		t.Error("track_added payload exposes audio_ref")
	}
}

func TestTrackAddedPayload_SingleCarriesTheTitleAsAlbum(t *testing.T) {
	_, p := addWithRecordedEvents(t, library.AddTrackInput{Title: "Single", Artist: "Artist"})

	if album := p["album"]; album != "Single" {
		t.Errorf("album = %v, want the title", album)
	}
}

func TestTrackAddedPayload_MatchesRESTDTOByteForByte(t *testing.T) {
	pub := &library.InMemoryRecordingPublisher{}
	svc := library.MustNewAddTrackService(library.NewInMemoryTrackRepo(), pub, &library.InMemoryScheduler{}, time.Now, ids.Real)

	status, restBody := postTrack(t, svc, librarytest.UserId(), `{
		"title": "Midnight City", "artist": "M83", "album": "Hurry Up, We're Dreaming",
		"duration_seconds": 240.5, "artwork_url": "https://cdn.example/art.jpg", "year": 2011,
		"genre": "electronic", "track_number": 4, "album_artist": "M83", "isrc": "USUM71100001",
		"featured_artists": [{"name": "Susanne Sundfor", "mbid": "11111111-2222-3333-4444-555555555555", "deezer_id": 4567}]
	}`)
	if status != 201 {
		t.Fatalf("POST /tracks status = %d, body %s", status, restBody)
	}

	payload := pub.LastFields(events.TypeTrackAddedToLibrary)
	if payload == nil || payload["track_id"] != payload["id"] {
		t.Fatalf("payload = %v, want track_id equal to id", payload)
	}
	delete(payload, "track_id")
	restJSON := canonicalJSON(t, restBody)
	if sseJSON := canonicalJSON(t, mustMarshal(t, payload)); restJSON != sseJSON {
		t.Errorf("SSE payload diverged from REST DTO\n REST: %s\n  SSE: %s", restJSON, sseJSON)
	}
	if !strings.Contains(restJSON, `"featured_artists":[{`) || !strings.Contains(restJSON, `"isrc":"USUM71100001"`) {
		t.Errorf("REST DTO = %s, want every saved field present so the comparison covers them", restJSON)
	}
}

func TestAddTrackService_RefusedScheduleFailsTrack(t *testing.T) {
	ctx := context.Background()
	repo := library.NewInMemoryTrackRepo()
	sched := &library.InMemoryScheduler{Err: errors.New("acquisition queue is full")}
	pub := &library.InMemoryRecordingPublisher{}
	svc := library.MustNewAddTrackService(repo, pub, sched, time.Now, ids.Real)

	out, err := svc.Execute(ctx, librarytest.UserId(), library.AddTrackInput{Title: "Track", Artist: "Artist", Album: "Album"})
	if err != nil {
		t.Fatalf("Execute = %v, want the track created despite the refused schedule", err)
	}
	if len(sched.TrackIds) != 1 {
		t.Fatalf("schedule attempts = %d, want 1", len(sched.TrackIds))
	}

	stored, _ := repo.GetByID(ctx, out.Track.ID(), out.Track.UserId())
	if stored == nil || stored.DownloadStatus() != library.AcquisitionFailed {
		t.Fatalf("stored track = %+v, want failed (not stranded pending)", stored)
	}
	if stored.FailureReason() == nil || *stored.FailureReason() != string(downloadfailure.FailureAcquisitionRefused) {
		t.Errorf("failure reason = %v, want %q", stored.FailureReason(), downloadfailure.FailureAcquisitionRefused)
	}
	if stored.AcquisitionStartedAt() != nil {
		t.Error("in-flight marker left set on a track whose job was never queued")
	}
	if got := pub.LastFields(events.TypeTrackAcquisitionFailed); got == nil || got["reason"] != string(downloadfailure.FailureAcquisitionRefused) {
		t.Errorf("track_acquisition_failed payload = %v, want reason %q", got, downloadfailure.FailureAcquisitionRefused)
	}
}

func TestAddTrackService_RefusedScheduleUnpersistedStaysPending(t *testing.T) {
	ctx := context.Background()
	repo := library.NewInMemoryTrackRepo()
	repo.ErrOnUpdate = errors.New("db down")
	pub := &library.InMemoryRecordingPublisher{}
	sched := &library.InMemoryScheduler{Err: errors.New("acquisition queue is full")}
	svc := library.MustNewAddTrackService(repo, pub, sched, time.Now, ids.Real)

	out, err := svc.Execute(ctx, librarytest.UserId(), library.AddTrackInput{Title: "Track", Artist: "Artist", Album: "Album"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Track.DownloadStatus() != library.AcquisitionPending || out.Track.AcquisitionStartedAt() == nil {
		t.Errorf("returned track = %v (started_at %v), want pending as stored", out.Track.DownloadStatus(), out.Track.AcquisitionStartedAt())
	}
	if pub.Last(events.TypeTrackAcquisitionFailed) != nil {
		t.Error("published track_acquisition_failed for a failure that was never persisted")
	}
}

func TestAddTrackService_AcceptedScheduleLeavesTrackPending(t *testing.T) {
	ctx := context.Background()
	repo := library.NewInMemoryTrackRepo()
	pub := &library.InMemoryRecordingPublisher{}
	svc := library.MustNewAddTrackService(repo, pub, &library.InMemoryScheduler{}, time.Now, ids.Real)

	out, err := svc.Execute(ctx, librarytest.UserId(), library.AddTrackInput{Title: "Track", Artist: "Artist", Album: "Album"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Track.DownloadStatus() != library.AcquisitionPending {
		t.Errorf("status = %v, want pending while the queued job runs", out.Track.DownloadStatus())
	}
	if pub.Last(events.TypeTrackAcquisitionFailed) != nil {
		t.Error("published track_acquisition_failed for an accepted schedule")
	}
}

func TestAddTrackService_ForwardsSourceURLToScheduler(t *testing.T) {
	ctx := context.Background()
	sched := &library.InMemoryScheduler{}
	svc := library.MustNewAddTrackService(library.NewInMemoryTrackRepo(), events.NoopPublisher(), sched, time.Now, ids.Real)

	scURL := "https://soundcloud.com/liltecca/fell-in-love"
	if _, err := svc.Execute(ctx, librarytest.UserId(), library.AddTrackInput{
		Title:     "Fell In Love",
		Artist:    "Lil Tecca",
		SourceURL: &scURL,
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(sched.SourceURLs) != 1 || sched.SourceURLs[0] != scURL {
		t.Fatalf("scheduler should receive source URL %q, got %v", scURL, sched.SourceURLs)
	}
}

func TestAddTrackService_NoSourceURL_ForwardsEmpty(t *testing.T) {
	ctx := context.Background()
	sched := &library.InMemoryScheduler{}
	svc := library.MustNewAddTrackService(library.NewInMemoryTrackRepo(), events.NoopPublisher(), sched, time.Now, ids.Real)

	if _, err := svc.Execute(ctx, librarytest.UserId(), library.AddTrackInput{Title: "Some Track", Artist: "Some Artist"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(sched.SourceURLs) != 1 || sched.SourceURLs[0] != "" {
		t.Fatalf("scheduler should receive an empty source URL, got %v", sched.SourceURLs)
	}
}

func assertRejectedBeforePersistence(t *testing.T, input library.AddTrackInput, wantErr string) {
	t.Helper()
	repo := library.NewInMemoryTrackRepo()
	out, err := library.MustNewAddTrackService(repo, events.NoopPublisher(), &library.InMemoryScheduler{}, time.Now, ids.Real).Execute(context.Background(), librarytest.UserId(), input)

	if err == nil {
		t.Fatalf("expected a validation error, got nil (out=%+v)", out)
	}
	dbtest.AssertValidationError(t, err)
	if !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("error = %q, want it to mention %q", err.Error(), wantErr)
	}
	if len(repo.Tracks) != 0 {
		t.Fatalf("stored %d tracks, want the invalid track rejected before persistence", len(repo.Tracks))
	}
}

func TestAddTrackService_ValidatesRanges(t *testing.T) {
	negDuration := -100.0
	posInfDuration := math.Inf(1)
	nanDuration := math.NaN()
	hugeDuration := math.MaxFloat64
	zeroTrackNumber := 0
	negTrackNumber := -3
	hugeTrackNumber := 3000000000
	farFutureYear := time.Now().UTC().Year() + 50
	ancientYear := 1000
	plausibleDuration := 215.0

	tests := []struct {
		name    string
		mutate  func(*library.AddTrackInput)
		wantErr string
	}{
		{"negative duration is rejected", func(in *library.AddTrackInput) { in.DurationSeconds = &negDuration }, "duration_seconds"},
		{"+Inf duration is rejected", func(in *library.AddTrackInput) { in.DurationSeconds = &posInfDuration }, "duration_seconds"},
		{"NaN duration is rejected", func(in *library.AddTrackInput) { in.DurationSeconds = &nanDuration }, "duration_seconds"},
		{"duration above the cap is rejected", func(in *library.AddTrackInput) { in.DurationSeconds = &hugeDuration }, "duration_seconds"},
		{"zero track number is rejected", func(in *library.AddTrackInput) { in.TrackNumber = &zeroTrackNumber }, "track_number"},
		{"negative track number is rejected", func(in *library.AddTrackInput) { in.TrackNumber = &negTrackNumber }, "track_number"},
		{"out-of-range track number is rejected", func(in *library.AddTrackInput) { in.TrackNumber = &hugeTrackNumber }, "track_number"},
		{"far-future year is rejected", func(in *library.AddTrackInput) { in.Year = &farFutureYear }, "year"},
		{"ancient year is rejected", func(in *library.AddTrackInput) { in.Year = &ancientYear }, "year"},
		{"a plausible duration does not excuse an ancient year", func(in *library.AddTrackInput) {
			in.DurationSeconds = &plausibleDuration
			in.Year = &ancientYear
		}, "year"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := library.AddTrackInput{Title: "Track", Artist: "Artist", Album: "Album"}
			tt.mutate(&input)
			assertRejectedBeforePersistence(t, input, tt.wantErr)
		})
	}
}

func TestAddTrackService_YearCeilingSitsOneYearPastTheClock(t *testing.T) {
	ctx := context.Background()
	userId := librarytest.UserId()
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
			repo := library.NewInMemoryTrackRepo()
			svc := library.MustNewAddTrackService(repo, events.NoopPublisher(), &library.InMemoryScheduler{}, func() time.Time { return pinned }, ids.Real)
			input := library.AddTrackInput{Title: "Track", Artist: "Artist", Album: "Album", Year: &tt.year}

			_, err := svc.Execute(ctx, userId, input)

			if tt.wantStored == 0 {
				dbtest.AssertValidationError(t, err)
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(repo.Tracks) != tt.wantStored {
				t.Fatalf("stored %d tracks, want %d for year %d", len(repo.Tracks), tt.wantStored, tt.year)
			}
		})
	}
}

func TestNewAddTrackService_RefusesAMissingCollaborator(t *testing.T) {
	repo := library.NewInMemoryTrackRepo()
	pub := events.NoopPublisher()
	sched := &library.InMemoryScheduler{}
	tests := []struct {
		name string
		make func() (*library.AddTrackService, error)
		want error
	}{
		{"repository", func() (*library.AddTrackService, error) {
			return library.NewAddTrackService(nil, pub, sched, time.Now, ids.Real)
		}, library.ErrTrackCollaboratorRequired},
		{"events", func() (*library.AddTrackService, error) {
			return library.NewAddTrackService(repo, nil, sched, time.Now, ids.Real)
		}, library.ErrTrackCollaboratorRequired},
		{"scheduler", func() (*library.AddTrackService, error) {
			return library.NewAddTrackService(repo, pub, nil, time.Now, ids.Real)
		}, library.ErrTrackCollaboratorRequired},
		{"clock", func() (*library.AddTrackService, error) {
			return library.NewAddTrackService(repo, pub, sched, nil, ids.Real)
		}, library.ErrTrackClockRequired},
		{"id source", func() (*library.AddTrackService, error) {
			return library.NewAddTrackService(repo, pub, sched, time.Now, nil)
		}, library.ErrTrackIDSourceRequired},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, err := tt.make()

			if !errors.Is(err, tt.want) || svc != nil {
				t.Fatalf("construction without %s = (%v, %v), want %v and no service", tt.name, svc, err, tt.want)
			}
			if !strings.Contains(err.Error(), "NewAddTrackService") {
				t.Fatalf("error %q does not name the constructor", err)
			}
		})
	}
}

func TestMustNewAddTrackService_PanicsOnANilClock(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("MustNewAddTrackService accepted a nil clock")
		}
	}()

	library.MustNewAddTrackService(library.NewInMemoryTrackRepo(), events.NoopPublisher(), &library.InMemoryScheduler{}, nil, ids.Real)
}

func TestAddTrackService_ValidatesFreeFormFields(t *testing.T) {
	oversized := strings.Repeat("x", 301)
	malformedURL := "not a url"
	schemelessURL := "example.com/song.mp3"
	ftpURL := "ftp://example.com/song.mp3"
	metadataURL := "http://169.254.169.254/latest/meta-data/"
	loopbackURL := "http://127.0.0.1/admin"

	tests := []struct {
		name    string
		mutate  func(*library.AddTrackInput)
		wantErr string
	}{
		{"oversized artwork_url is rejected", func(in *library.AddTrackInput) { in.ArtworkURL = &oversized }, "artwork_url"},
		{"oversized album is rejected", func(in *library.AddTrackInput) { in.Album = oversized }, "album"},
		{"oversized genre is rejected", func(in *library.AddTrackInput) { in.Genre = &oversized }, "genre"},
		{"oversized album_artist is rejected", func(in *library.AddTrackInput) { in.AlbumArtist = &oversized }, "album_artist"},
		{"oversized isrc is rejected", func(in *library.AddTrackInput) { in.ISRC = &oversized }, "isrc"},
		{"malformed source_url is rejected", func(in *library.AddTrackInput) { in.SourceURL = &malformedURL }, "source_url"},
		{"schemeless source_url is rejected", func(in *library.AddTrackInput) { in.SourceURL = &schemelessURL }, "source_url"},
		{"non-http source_url is rejected", func(in *library.AddTrackInput) { in.SourceURL = &ftpURL }, "source_url"},
		{"metadata-endpoint source_url is rejected", func(in *library.AddTrackInput) { in.SourceURL = &metadataURL }, "source_url must target a public host"},
		{"loopback source_url is rejected", func(in *library.AddTrackInput) { in.SourceURL = &loopbackURL }, "source_url must target a public host"},
		{"oversized source_url is rejected", func(in *library.AddTrackInput) { in.SourceURL = &oversized }, "source_url"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := library.AddTrackInput{Title: "Track", Artist: "Artist", Album: "Album"}
			tt.mutate(&input)
			assertRejectedBeforePersistence(t, input, tt.wantErr)
		})
	}
}

func TestAddTrackService_AcceptsValidFreeFormFields(t *testing.T) {
	artwork := "https://cdn.example.com/art.jpg"
	genre := "Jazz"
	albumArtist := "Various Artists"
	isrc := "USUM71703861"
	sourceURL := "https://example.com/song.mp3"

	out, err := library.MustNewAddTrackService(library.NewInMemoryTrackRepo(), events.NoopPublisher(), &library.InMemoryScheduler{}, time.Now, ids.Real).Execute(context.Background(), librarytest.UserId(), library.AddTrackInput{
		Title:       "Track",
		Artist:      "Artist",
		Album:       "Album",
		ArtworkURL:  &artwork,
		Genre:       &genre,
		AlbumArtist: &albumArtist,
		ISRC:        &isrc,
		SourceURL:   &sourceURL,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Track.ArtworkURL() == nil || *out.Track.ArtworkURL() != artwork {
		t.Errorf("ArtworkURL = %v, want %v", out.Track.ArtworkURL(), artwork)
	}
	if out.Track.ISRC() == nil || *out.Track.ISRC() != isrc {
		t.Errorf("ISRC = %v, want %v", out.Track.ISRC(), isrc)
	}
}

func TestAddTrackService_AcceptsValidRanges(t *testing.T) {
	duration := 210.5
	trackNumber := 7
	year := time.Now().UTC().Year()

	out, err := library.MustNewAddTrackService(library.NewInMemoryTrackRepo(), events.NoopPublisher(), &library.InMemoryScheduler{}, time.Now, ids.Real).Execute(context.Background(), librarytest.UserId(), library.AddTrackInput{
		Title:           "Track",
		Artist:          "Artist",
		Album:           "Album",
		DurationSeconds: &duration,
		TrackNumber:     &trackNumber,
		Year:            &year,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Track.DurationSeconds() == nil || *out.Track.DurationSeconds() != duration {
		t.Errorf("DurationSeconds = %v, want %v", out.Track.DurationSeconds(), duration)
	}
	if out.Track.TrackNumber() == nil || *out.Track.TrackNumber() != trackNumber {
		t.Errorf("TrackNumber = %v, want %v", out.Track.TrackNumber(), trackNumber)
	}
	if out.Track.Year() == nil || *out.Track.Year() != year {
		t.Errorf("Year = %v, want %v", out.Track.Year(), year)
	}
}

func TestAddTrackService_ScheduleBoundedByTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sched := &library.InMemoryStuckScheduler{}
		svc := library.MustNewAddTrackService(library.NewInMemoryTrackRepo(), events.NoopPublisher(), sched, time.Now, ids.Real)

		start := time.Now()
		ctx, cancel := context.WithCancel(context.Background())
		go func() { time.Sleep(20 * time.Millisecond); cancel() }()
		if _, err := svc.Execute(ctx, librarytest.UserId(), library.AddTrackInput{Title: "T", Artist: "A", Album: "B"}); err != nil {
			t.Fatalf("Execute: %v", err)
		}
		library.AssertScheduleDeadline(t, sched, start, addTrackScheduleBound, clock.Now)
	})
}

func TestAddTrackService_ScheduleTimeoutFailsTrack(t *testing.T) {
	svc := library.MustNewAddTrackService(library.NewInMemoryTrackRepo(), events.NoopPublisher(), &library.InMemoryStuckScheduler{}, time.Now, ids.Real)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	out, err := svc.Execute(ctx, librarytest.UserId(), library.AddTrackInput{Title: "T", Artist: "A", Album: "B"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Track.DownloadStatus() != library.AcquisitionFailed {
		t.Errorf("status = %v, want failed after a timed-out schedule", out.Track.DownloadStatus())
	}
	if out.Track.FailureReason() == nil || *out.Track.FailureReason() != string(downloadfailure.FailureAcquisitionRefused) {
		t.Errorf("failure reason = %v, want %q", out.Track.FailureReason(), downloadfailure.FailureAcquisitionRefused)
	}
}

func TestAddTrackService_RefusedScheduleWithFailedPersistLeavesTrackClaimable(t *testing.T) {
	ctx := context.Background()
	repo := library.NewInMemoryTrackRepo()
	repo.ErrOnUpdate = errors.New("db down")
	sched := &library.InMemoryScheduler{Err: errors.New("acquisition queue is full")}
	svc := library.MustNewAddTrackService(repo, events.NoopPublisher(), sched, time.Now, ids.Real)

	out, err := svc.Execute(ctx, librarytest.UserId(), library.AddTrackInput{Title: "Track", Artist: "Artist", Album: "Album"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	stored, err := repo.GetByID(ctx, out.Track.ID(), out.Track.UserId())
	if err != nil || stored == nil {
		t.Fatalf("GetByID: track = %v, err = %v", stored, err)
	}
	if stored.DownloadStatus() == library.AcquisitionPending && !repo.Claimable(stored.ID()) {
		t.Fatal("stored track is pending and unclaimable: nothing will ever acquire or fail it")
	}
}

func TestAddTrackService_Execute_KeepsTheClientDurationAsCatalogLength(t *testing.T) {
	duration := 235.0

	out, err := library.MustNewAddTrackService(library.NewInMemoryTrackRepo(), events.NoopPublisher(), &library.InMemoryScheduler{}, time.Now, ids.Real).Execute(context.Background(), librarytest.UserId(), library.AddTrackInput{
		Title: "Track", Artist: "Artist", Album: "Album", DurationSeconds: &duration,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if out.Track.CatalogDurationSeconds() == nil || *out.Track.CatalogDurationSeconds() != duration {
		t.Errorf("CatalogDurationSeconds = %v, want %v", out.Track.CatalogDurationSeconds(), duration)
	}
	if out.Track.DurationSeconds() == nil || *out.Track.DurationSeconds() != duration {
		t.Errorf("DurationSeconds = %v, want %v", out.Track.DurationSeconds(), duration)
	}
}

func TestAddTrackService_PinsTheCatalogIdentity(t *testing.T) {
	explicit := false
	mbid := "5B11F4CE-A62D-471E-81FC-A69A8278C7DA"
	input := library.AddTrackInput{
		Title: "Diamonds", Artist: "Rihanna", Album: "Unapologetic",
		CatalogMBID: &mbid, CatalogExplicit: &explicit,
		CatalogSources: []library.CatalogSource{
			{Provider: "deezer", ExternalID: "1"},
			{Provider: "deezer", ExternalID: "1"},
			{Provider: "spotify", ExternalID: "s"},
		},
	}

	out, err := library.MustNewAddTrackService(library.NewInMemoryTrackRepo(), events.NoopPublisher(), &library.InMemoryScheduler{}, time.Now, ids.Real).Execute(context.Background(), librarytest.UserId(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got := out.Track
	if got.CatalogMBID() == nil || *got.CatalogMBID() != "5b11f4ce-a62d-471e-81fc-a69a8278c7da" {
		t.Errorf("CatalogMBID = %v, want the lowercased uuid", got.CatalogMBID())
	}
	if got.CatalogExplicit() == nil || *got.CatalogExplicit() {
		t.Errorf("CatalogExplicit = %v, want false", got.CatalogExplicit())
	}
	if len(got.CatalogSources()) != 2 {
		t.Errorf("CatalogSources = %+v, want 2 after dropping the duplicate", got.CatalogSources())
	}
}

func TestAddTrackService_RejectsInvalidCatalogIdentity(t *testing.T) {
	badMBID := "not-a-uuid"
	tooMany := make([]library.CatalogSource, library.MaxCatalogSources+1)
	for i := range tooMany {
		tooMany[i] = library.CatalogSource{Provider: "deezer", ExternalID: fmt.Sprint(i)}
	}
	tests := []struct {
		name   string
		mutate func(*library.AddTrackInput)
	}{
		{"bad mbid", func(in *library.AddTrackInput) { in.CatalogMBID = &badMBID }},
		{"unknown provider", func(in *library.AddTrackInput) {
			in.CatalogSources = []library.CatalogSource{{Provider: "napster", ExternalID: "1"}}
		}},
		{"too many sources", func(in *library.AddTrackInput) { in.CatalogSources = tooMany }},
		{"empty external id", func(in *library.AddTrackInput) {
			in.CatalogSources = []library.CatalogSource{{Provider: "deezer"}}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := library.NewInMemoryTrackRepo()
			input := library.AddTrackInput{Title: "T", Artist: "A", Album: "B"}
			tt.mutate(&input)

			out, err := library.MustNewAddTrackService(repo, events.NoopPublisher(), &library.InMemoryScheduler{}, time.Now, ids.Real).Execute(context.Background(), librarytest.UserId(), input)

			dbtest.AssertValidationError(t, err)
			if out != nil || len(repo.Tracks) != 0 {
				t.Fatalf("a refused save must not insert: out=%v stored=%d", out, len(repo.Tracks))
			}
		})
	}
}

func TestAddTrackService_KeepsThePickedSoundCloudURLOnItsCatalogSource(t *testing.T) {
	repo := library.NewInMemoryTrackRepo()
	userId := librarytest.UserId()
	pick := "https://soundcloud.com/artist/track"
	input := library.AddTrackInput{
		Title: "Track", Artist: "Artist", Album: "Album",
		CatalogSources: []library.CatalogSource{{Provider: "deezer", ExternalID: "1"}},
		SourceURL:      &pick,
	}

	out, err := library.MustNewAddTrackService(repo, events.NoopPublisher(), &library.InMemoryScheduler{}, time.Now, ids.Real).Execute(context.Background(), userId, input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	stored, err := repo.GetByID(context.Background(), out.Track.ID(), userId)
	if err != nil || stored == nil {
		t.Fatalf("GetByID: %v, %v", stored, err)
	}
	want := []library.CatalogSource{
		{Provider: "deezer", ExternalID: "1"},
		{Provider: "soundcloud", ExternalID: pick, URL: pick},
	}
	if !reflect.DeepEqual(stored.CatalogSources(), want) {
		t.Errorf("CatalogSources = %+v, want %+v", stored.CatalogSources(), want)
	}
}

func TestAddTrackService_FullCatalogSourcesWithPickedSoundCloudURLStillAdds(t *testing.T) {
	repo := library.NewInMemoryTrackRepo()
	userId := librarytest.UserId()
	pick := "https://soundcloud.com/artist/track"
	sources := make([]library.CatalogSource, library.MaxCatalogSources)
	for i := range sources {
		sources[i] = library.CatalogSource{Provider: "deezer", ExternalID: strconv.Itoa(i + 1)}
	}
	input := library.AddTrackInput{Title: "Track", Artist: "Artist", Album: "Album", CatalogSources: sources, SourceURL: &pick}

	out, err := library.MustNewAddTrackService(repo, events.NoopPublisher(), &library.InMemoryScheduler{}, time.Now, ids.Real).Execute(context.Background(), userId, input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	stored, err := repo.GetByID(context.Background(), out.Track.ID(), userId)
	if err != nil || stored == nil {
		t.Fatalf("GetByID: %v, %v", stored, err)
	}
	if !reflect.DeepEqual(stored.CatalogSources(), sources) {
		t.Errorf("CatalogSources = %+v, want unchanged", stored.CatalogSources())
	}
}
