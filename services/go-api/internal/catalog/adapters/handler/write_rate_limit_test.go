package handler

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/service"
	"altune/go-api/internal/shared"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// writeRig is the real /tracks and /playlists route tables behind the real
// auth middleware, over in-memory repositories. Reproduces #2200: before the
// throttle, one authenticated caller could create rows on these routes as fast
// as the API answered.
type writeRig struct {
	router    chi.Router
	tracks    *catalogtest.TrackRepo
	playlists *catalogtest.PlaylistRepo
}

func newWriteRig(trackOpts []func(*TrackHandler), playlistOpts []func(*PlaylistHandler)) *writeRig {
	tracks := catalogtest.NewTrackRepo()
	playlists := catalogtest.NewPlaylistRepo()
	featured := NewFeaturedArtistHandler(
		service.NewBackfillFeaturedService(tracks, tracks, fakeResolver{}),
		service.NewListFeaturingService(tracks),
	)
	trackH := NewTrackHandler(
		service.NewAddTrackService(tracks),
		service.NewListTracksService(tracks),
		service.NewGetTrackStatusService(tracks),
		service.NewDeleteTrackService(tracks, catalogtest.NewAudioStore()),
		service.NewSetTrackNumberService(tracks),
		featured,
		trackOpts...,
	)
	playlistH := NewPlaylistHandler(
		service.NewPlaylistLifecycleService(playlists),
		service.NewPlaylistMembershipService(playlists, tracks),
		playlistOpts...,
	)

	r := chi.NewRouter()
	r.Use(auth.Middleware(verifyBearerAsUser))
	r.Route("/tracks", func(r chi.Router) { r.Mount("/", trackH.Routes()) })
	r.Mount("/playlists", playlistH.Routes())
	return &writeRig{router: r, tracks: tracks, playlists: playlists}
}

// newThrottledWriteRig gives both handlers the same wound-down budget off one
// fake clock, so a test crosses it in a handful of requests.
func newThrottledWriteRig(limit AudioRateLimit, now func() time.Time) *writeRig {
	return newWriteRig(
		[]func(*TrackHandler){WithTrackWriteRateLimit(limit), withTrackWriteClock(now)},
		[]func(*PlaylistHandler){WithPlaylistWriteRateLimit(limit), withPlaylistWriteClock(now)},
	)
}

func (rig *writeRig) post(user shared.UserId, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+user.String())
	rec := httptest.NewRecorder()
	rig.router.ServeHTTP(rec, req)
	return rec
}

func (rig *writeRig) createTrack(user shared.UserId, title string) *httptest.ResponseRecorder {
	return rig.post(user, "/tracks/", fmt.Sprintf(`{"title":%q,"artist":"Artist","album":"Album"}`, title))
}

func (rig *writeRig) createPlaylist(user shared.UserId, name string) *httptest.ResponseRecorder {
	return rig.post(user, "/playlists/", fmt.Sprintf(`{"name":%q}`, name))
}

func (rig *writeRig) addTrackToPlaylist(user shared.UserId, playlistId domain.PlaylistId, trackId domain.TrackId) *httptest.ResponseRecorder {
	return rig.post(user, "/playlists/"+playlistId.String()+"/tracks",
		fmt.Sprintf(`{"track_id":%q}`, trackId.String()))
}

func (rig *writeRig) addTracksToPlaylist(user shared.UserId, playlistId domain.PlaylistId, trackId domain.TrackId) *httptest.ResponseRecorder {
	return rig.post(user, "/playlists/"+playlistId.String()+"/tracks/batch",
		fmt.Sprintf(`{"track_ids":[%q]}`, trackId.String()))
}

// seedPlaylistAndTrack gives user an owned playlist and an owned track, so a
// membership add is refused by the throttle alone and never by ownership.
func (rig *writeRig) seedPlaylistAndTrack(t *testing.T, user shared.UserId) (domain.PlaylistId, domain.TrackId) {
	t.Helper()
	playlist, err := domain.NewPlaylist(user, "Seeded", time.Now())
	if err != nil {
		t.Fatalf("seed playlist: %v", err)
	}
	track, err := domain.NewTrack(user, "Seeded Track", "Artist", "Album")
	if err != nil {
		t.Fatalf("seed track: %v", err)
	}
	rig.playlists.Seed(playlist)
	rig.tracks.Seed(track)
	return playlist.ID, track.ID
}

func assertWriteThrottled(t *testing.T, rec *httptest.ResponseRecorder, wantRetryAfter string) {
	t.Helper()
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 (body: %s)", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "catalog.write_rate_limited")
	if got := rec.Header().Get("Retry-After"); got != wantRetryAfter {
		t.Fatalf("Retry-After = %q, want %q", got, wantRetryAfter)
	}
}

// TestCreateTrack_ThrottlesPerUser holds POST /tracks to a per-user budget:
// the creates past the burst are refused with 429 and a Retry-After, and none
// of them reaches the repository. Every title is distinct, so dedup cannot be
// what stops the row.
func TestCreateTrack_ThrottlesPerUser(t *testing.T) {
	clock := newAudioFakeClock()
	limit := AudioRateLimit{Every: 2 * time.Second, Burst: 5}
	rig := newThrottledWriteRig(limit, clock.now)
	user := shared.NewUserId(uuid.New())

	for i := range limit.Burst {
		rec := rig.createTrack(user, fmt.Sprintf("Inside %d", i))
		if rec.Code != http.StatusCreated {
			t.Fatalf("create %d inside the burst must store, got %d (%s)", i, rec.Code, rec.Body.String())
		}
	}
	for i := range 20 {
		assertWriteThrottled(t, rig.createTrack(user, fmt.Sprintf("Flood %d", i)), "2")
	}

	if len(rig.tracks.Tracks) != limit.Burst {
		t.Fatalf("stored tracks = %d, want %d: a throttled create must not insert", len(rig.tracks.Tracks), limit.Burst)
	}

	clock.advance(limit.Every)
	if rec := rig.createTrack(user, "After refill"); rec.Code != http.StatusCreated {
		t.Fatalf("a refilled token must admit the next create, got %d (%s)", rec.Code, rec.Body.String())
	}
}

// TestPlaylistWrites_ThrottlePerUser holds the three row-creating playlist
// routes to one shared per-user budget — creating a playlist and both
// membership adds — while leaving another principal's budget untouched.
func TestPlaylistWrites_ThrottlePerUser(t *testing.T) {
	clock := newAudioFakeClock()
	limit := AudioRateLimit{Every: time.Second, Burst: 3}
	rig := newThrottledWriteRig(limit, clock.now)
	noisy, quiet := shared.NewUserId(uuid.New()), shared.NewUserId(uuid.New())
	playlistId, trackId := rig.seedPlaylistAndTrack(t, noisy)

	if rec := rig.createPlaylist(noisy, "First"); rec.Code != http.StatusCreated {
		t.Fatalf("create inside the burst must store, got %d (%s)", rec.Code, rec.Body.String())
	}
	if rec := rig.addTrackToPlaylist(noisy, playlistId, trackId); rec.Code != http.StatusNoContent {
		t.Fatalf("add inside the burst must store, got %d (%s)", rec.Code, rec.Body.String())
	}
	if rec := rig.addTracksToPlaylist(noisy, playlistId, trackId); rec.Code != http.StatusOK {
		t.Fatalf("batch add inside the burst must be served, got %d (%s)", rec.Code, rec.Body.String())
	}

	assertWriteThrottled(t, rig.createPlaylist(noisy, "Past the burst"), "1")
	assertWriteThrottled(t, rig.addTrackToPlaylist(noisy, playlistId, trackId), "1")
	assertWriteThrottled(t, rig.addTracksToPlaylist(noisy, playlistId, trackId), "1")

	if rec := rig.createPlaylist(quiet, "Untouched"); rec.Code != http.StatusCreated {
		t.Fatalf("another user's write budget must be untouched, got %d (%s)", rec.Code, rec.Body.String())
	}
}

// TestWriteRateLimits_DefaultsAdmitRealClientTraffic replays the client's
// heaviest legitimate write patterns against the default budgets on one
// account, with no time passing between calls; not one request may be
// throttled.
//
//   - "Save all" on a 100-track compilation: one POST /tracks per unowned
//     track, four in flight, then the same again on a second album.
//   - Adding a track to every playlist it owns from the add-to-playlist sheet:
//     one batch call per playlist.
func TestWriteRateLimits_DefaultsAdmitRealClientTraffic(t *testing.T) {
	clock := newAudioFakeClock()
	rig := newWriteRig(
		[]func(*TrackHandler){withTrackWriteClock(clock.now)},
		[]func(*PlaylistHandler){withPlaylistWriteClock(clock.now)},
	)
	user := shared.NewUserId(uuid.New())
	playlistId, trackId := rig.seedPlaylistAndTrack(t, user)

	for i := range 150 {
		rec := rig.createTrack(user, fmt.Sprintf("Compilation Track %d", i))
		if rec.Code != http.StatusCreated {
			t.Fatalf("save-all track %d throttled: %d (%s)", i, rec.Code, rec.Body.String())
		}
	}

	for i := range 40 {
		if rec := rig.addTracksToPlaylist(user, playlistId, trackId); rec.Code != http.StatusOK {
			t.Fatalf("add-to-playlist %d throttled: %d (%s)", i, rec.Code, rec.Body.String())
		}
	}
}

// TestPlaylistWrites_LeaveReadsAndRemovalsUnthrottled pins the throttle to the
// routes that grow the account: a caller whose write budget is spent can still
// read its playlists and remove tracks, which is the one way back under a cap.
func TestPlaylistWrites_LeaveReadsAndRemovalsUnthrottled(t *testing.T) {
	clock := newAudioFakeClock()
	rig := newThrottledWriteRig(AudioRateLimit{Every: time.Hour, Burst: 1}, clock.now)
	user := shared.NewUserId(uuid.New())
	playlistId, trackId := rig.seedPlaylistAndTrack(t, user)

	if rec := rig.addTrackToPlaylist(user, playlistId, trackId); rec.Code != http.StatusNoContent {
		t.Fatalf("the one budgeted add must store, got %d (%s)", rec.Code, rec.Body.String())
	}
	assertWriteThrottled(t, rig.addTrackToPlaylist(user, playlistId, trackId), "3600")

	req := httptest.NewRequest(http.MethodGet, "/playlists/", nil)
	req.Header.Set("Authorization", "Bearer "+user.String())
	list := httptest.NewRecorder()
	rig.router.ServeHTTP(list, req)
	if list.Code != http.StatusOK {
		t.Fatalf("list with a spent write budget = %d, want 200 (%s)", list.Code, list.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/playlists/"+playlistId.String()+"/tracks/"+trackId.String(), nil)
	req.Header.Set("Authorization", "Bearer "+user.String())
	remove := httptest.NewRecorder()
	rig.router.ServeHTTP(remove, req)
	if remove.Code != http.StatusNoContent {
		t.Fatalf("remove with a spent write budget = %d, want 204 (%s)", remove.Code, remove.Body.String())
	}
}
