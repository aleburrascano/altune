package handler

import (
	"altune/go-api/internal/catalog/catalogtest"
	featuredservice "altune/go-api/internal/catalog/internal/core/featured"
	playlistservice "altune/go-api/internal/catalog/internal/core/playlist"
	trackservice "altune/go-api/internal/catalog/service/track"
	shared "altune/go-api/internal/shared/kernel"
	"altune/go-api/internal/shared/requestuser"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	catdomain "altune/go-api/internal/catalog/domain"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

var (
	testUserUUID = uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	testUserId   = shared.NewUserId(testUserUUID)
)

func asTestUser(string) (shared.UserId, bool) { return testUserId, true }

func bearerAsUser(token string) (shared.UserId, bool) {
	id, err := uuid.Parse(token)
	return shared.NewUserId(id), err == nil
}

type audioFakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newAudioFakeClock() *audioFakeClock { return &audioFakeClock{t: time.Unix(1_700_000_000, 0)} }

func (c *audioFakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *audioFakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func bearerUser(userFor func(token string) (shared.UserId, bool)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
				if id, known := userFor(token); known {
					r = r.WithContext(requestuser.ContextWithUserID(r.Context(), id))
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

func serve(t *testing.T, router chi.Router, method, path string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, body)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer fake-token")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func serveNoAuth(t *testing.T, router chi.Router, method, path string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, body)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func jsonBody(t *testing.T, v any) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	if err := json.NewEncoder(buf).Encode(v); err != nil {
		t.Fatalf("jsonBody: %v", err)
	}
	return buf
}

func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder, dst any) {
	t.Helper()
	if err := json.NewDecoder(rec.Body).Decode(dst); err != nil {
		t.Fatalf("decodeJSON: %v (body: %s)", err, rec.Body.String())
	}
}

func makeTrack(userId shared.UserId, title, artist, album string) *catdomain.Track {
	t, _ := catdomain.NewTrack(userId, title, artist, album)
	return t
}

func makePlaylist(userId shared.UserId, name string) *catdomain.Playlist {
	p, _ := catdomain.NewPlaylist(userId, name, time.Now())
	return p
}

func buildTrackHandler(trackRepo *catalogtest.TrackRepo, scheduler *catalogtest.Scheduler) (*TrackHandler, chi.Router) {
	var addOpts []func(*trackservice.AddTrackService)
	if scheduler != nil {
		addOpts = append(addOpts, trackservice.WithAcquisitionScheduler(scheduler))
	}
	addSvc := trackservice.NewAddTrackService(trackRepo, addOpts...)
	listSvc := trackservice.NewListTracksService(trackRepo)
	deleteSvc := trackservice.NewDeleteTrackService(trackRepo, catalogtest.NewAudioStore())
	setTrackNumberSvc := trackservice.NewSetTrackNumberService(trackRepo)

	getStatusSvc := trackservice.NewGetTrackStatusService(trackRepo)
	backfillSvc := featuredservice.NewBackfillFeaturedService(trackRepo, trackRepo, catalogtest.FeaturedResolver{})
	listFeaturingSvc := featuredservice.NewListFeaturingService(trackRepo)
	featuredH := NewFeaturedArtistHandler(backfillSvc, listFeaturingSvc)
	h := NewTrackHandler(addSvc, listSvc, getStatusSvc, deleteSvc, setTrackNumberSvc, featuredH)
	r := chi.NewRouter()
	r.Use(bearerUser(asTestUser))
	r.Route("/tracks", func(r chi.Router) {
		r.Mount("/", h.Routes())
	})
	return h, r
}

func buildPlaylistHandler(plRepo *catalogtest.PlaylistRepo, trRepo *catalogtest.TrackRepo) (*PlaylistHandler, chi.Router) {
	lifecycleSvc := playlistservice.NewPlaylistLifecycleService(plRepo)
	membershipSvc := playlistservice.NewPlaylistMembershipService(plRepo, trRepo)
	h := NewPlaylistHandler(lifecycleSvc, membershipSvc)
	r := chi.NewRouter()
	r.Use(bearerUser(asTestUser))
	r.Mount("/playlists", h.Routes())
	return h, r
}

func assertStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Errorf("status = %d, want %d (body: %s)", rec.Code, want, rec.Body.String())
	}
}

func assertErrorCode(t *testing.T, rec *httptest.ResponseRecorder, want string) {
	t.Helper()
	var body struct {
		Code string `json:"code"`
	}
	decodeJSON(t, rec, &body)
	if body.Code != want {
		t.Errorf("code = %q, want %q", body.Code, want)
	}
}

func assertJSON(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" && ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

type writeRig struct {
	router    chi.Router
	tracks    *catalogtest.TrackRepo
	playlists *catalogtest.PlaylistRepo
}

func newWriteRig(trackOpts []func(*TrackHandler), playlistOpts []func(*PlaylistHandler)) *writeRig {
	tracks := catalogtest.NewTrackRepo()
	playlists := catalogtest.NewPlaylistRepo()
	featured := NewFeaturedArtistHandler(
		featuredservice.NewBackfillFeaturedService(tracks, tracks, catalogtest.FeaturedResolver{}),
		featuredservice.NewListFeaturingService(tracks),
	)
	trackH := NewTrackHandler(
		trackservice.NewAddTrackService(tracks),
		trackservice.NewListTracksService(tracks),
		trackservice.NewGetTrackStatusService(tracks),
		trackservice.NewDeleteTrackService(tracks, catalogtest.NewAudioStore()),
		trackservice.NewSetTrackNumberService(tracks),
		featured,
		trackOpts...,
	)
	playlistH := NewPlaylistHandler(
		playlistservice.NewPlaylistLifecycleService(playlists),
		playlistservice.NewPlaylistMembershipService(playlists, tracks),
		playlistOpts...,
	)

	r := chi.NewRouter()
	r.Use(bearerUser(bearerAsUser))
	r.Route("/tracks", func(r chi.Router) { r.Mount("/", trackH.Routes()) })
	r.Mount("/playlists", playlistH.Routes())
	return &writeRig{router: r, tracks: tracks, playlists: playlists}
}

func newThrottledWriteRig(limit AudioRateLimit, now func() time.Time) *writeRig {
	return newWriteRig(
		[]func(*TrackHandler){WithTrackWriteRateLimit(limit), WithTrackWriteLimiterClock(now)},
		[]func(*PlaylistHandler){WithPlaylistWriteRateLimit(limit), WithPlaylistWriteLimiterClock(now)},
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

func (rig *writeRig) addTrackToPlaylist(user shared.UserId, playlistId catdomain.PlaylistId, trackId catdomain.TrackId) *httptest.ResponseRecorder {
	return rig.post(user, "/playlists/"+playlistId.String()+"/tracks",
		fmt.Sprintf(`{"track_id":%q}`, trackId.String()))
}

func (rig *writeRig) addTracksToPlaylist(user shared.UserId, playlistId catdomain.PlaylistId, trackId catdomain.TrackId) *httptest.ResponseRecorder {
	return rig.post(user, "/playlists/"+playlistId.String()+"/tracks/batch",
		fmt.Sprintf(`{"track_ids":[%q]}`, trackId.String()))
}

func (rig *writeRig) seedPlaylistAndTrack(t *testing.T, user shared.UserId) (catdomain.PlaylistId, catdomain.TrackId) {
	t.Helper()
	playlist, err := catdomain.NewPlaylist(user, "Seeded", time.Now())
	if err != nil {
		t.Fatalf("seed playlist: %v", err)
	}
	track, err := catdomain.NewTrack(user, "Seeded Track", "Artist", "Album")
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
