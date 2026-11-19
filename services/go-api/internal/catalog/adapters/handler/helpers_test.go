package handler_test

import (
	"altune/go-api/internal/catalog"
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

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func asBearer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
			if id, err := uuid.Parse(token); err == nil {
				r = r.WithContext(requestuser.ContextWithUserID(r.Context(), shared.NewUserId(id)))
			}
		}
		next.ServeHTTP(w, r)
	})
}

func trackRoutes(repo *catalog.InMemoryTrackRepo, opts ...func(*catalog.TrackHandler)) chi.Router {
	return trackRoutesWithBackfill(repo, catalog.NewBackfillFeaturedService(repo, repo, catalog.InMemoryFeaturedResolver{}), opts...)
}

func trackRoutesWithBackfill(repo *catalog.InMemoryTrackRepo, backfill *catalog.BackfillFeaturedService, opts ...func(*catalog.TrackHandler)) chi.Router {
	tracks := catalog.NewTrackHandler(
		catalog.NewAddTrackService(repo),
		catalog.NewListTracksService(repo),
		catalog.NewGetTrackStatusService(repo),
		catalog.NewDeleteTrackService(repo, catalog.NewInMemoryAudioStore()),
		catalog.NewSetTrackNumberService(repo),
		catalog.NewFeaturedArtistHandler(backfill, catalog.NewListFeaturingService(repo)),
		opts...,
	)
	router := chi.NewRouter()
	router.Use(asBearer)
	router.Route("/tracks", func(r chi.Router) { r.Mount("/", tracks.Routes()) })
	return router
}

func sendAs(router http.Handler, req *http.Request, user shared.UserId) *httptest.ResponseRecorder {
	req.Header.Set("Authorization", "Bearer "+user.String())
	return sendAnonymous(router, req)
}

func sendAnonymous(router http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func serve(router http.Handler, method, path string) *httptest.ResponseRecorder {
	return sendAs(router, httptest.NewRequest(method, path, nil), catalog.FixtureUserId())
}

func jsonRequest(t *testing.T, method, path string, v any) *http.Request {
	t.Helper()
	body, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("encode request body: %v", err)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func postJSON(t *testing.T, router http.Handler, path string, v any) *httptest.ResponseRecorder {
	t.Helper()
	return sendAs(router, jsonRequest(t, http.MethodPost, path, v), catalog.FixtureUserId())
}

func assertStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Errorf("status = %d, want %d (body: %s)", rec.Code, want, rec.Body.String())
	}
}

func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder, dst any) {
	t.Helper()
	if err := json.NewDecoder(rec.Body).Decode(dst); err != nil {
		t.Fatalf("decodeJSON: %v (body: %s)", err, rec.Body.String())
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

func assertPrivateAudioHeaders(t *testing.T, rec *httptest.ResponseRecorder, wantCacheControl string) {
	t.Helper()
	if got := rec.Header().Get("Cache-Control"); got != wantCacheControl {
		t.Errorf("Cache-Control = %q, want %q", got, wantCacheControl)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
}

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock() *fakeClock { return &fakeClock{t: time.Unix(1_700_000_000, 0)} }

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func assertThrottled(t *testing.T, rec *httptest.ResponseRecorder, wantCode, wantRetryAfter string) {
	t.Helper()
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 (body: %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"`+wantCode+`"`) {
		t.Fatalf("throttled body must carry the %s code, got %q", wantCode, rec.Body.String())
	}
	if got := rec.Header().Get("Retry-After"); got != wantRetryAfter {
		t.Fatalf("Retry-After = %q, want %q", got, wantRetryAfter)
	}
}

func assertWriteThrottled(t *testing.T, rec *httptest.ResponseRecorder, wantRetryAfter string) {
	t.Helper()
	assertThrottled(t, rec, "catalog.write_rate_limited", wantRetryAfter)
}

func assertJSON(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" && ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

func jsonBody(t *testing.T, v any) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	if err := json.NewEncoder(buf).Encode(v); err != nil {
		t.Fatalf("jsonBody: %v", err)
	}
	return buf
}

func request(t *testing.T, router http.Handler, method, path string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, body)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return sendAs(router, req, catalog.FixtureUserId())
}

func requestAnonymous(t *testing.T, router http.Handler, method, path string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	return sendAnonymous(router, httptest.NewRequest(method, path, body))
}

func makeTrack(userId shared.UserId, title, artist, album string) *catalog.Track {
	track, err := catalog.NewTrack(userId, title, artist, album)
	if err != nil {
		panic(fmt.Sprintf("make track %q: %v", title, err))
	}
	return track
}

func makePlaylist(userId shared.UserId, name string) *catalog.Playlist {
	playlist, err := catalog.NewPlaylist(userId, name, time.Now())
	if err != nil {
		panic(fmt.Sprintf("make playlist %q: %v", name, err))
	}
	return playlist
}

func buildTrackHandler(repo *catalog.InMemoryTrackRepo, scheduler *catalog.InMemoryScheduler) chi.Router {
	var addOpts []func(*catalog.AddTrackService)
	if scheduler != nil {
		addOpts = append(addOpts, catalog.WithAcquisitionScheduler(scheduler))
	}
	tracks := catalog.NewTrackHandler(
		catalog.NewAddTrackService(repo, addOpts...),
		catalog.NewListTracksService(repo),
		catalog.NewGetTrackStatusService(repo),
		catalog.NewDeleteTrackService(repo, catalog.NewInMemoryAudioStore()),
		catalog.NewSetTrackNumberService(repo),
		catalog.NewFeaturedArtistHandler(
			catalog.NewBackfillFeaturedService(repo, repo, catalog.InMemoryFeaturedResolver{}),
			catalog.NewListFeaturingService(repo),
		),
	)
	router := chi.NewRouter()
	router.Use(asBearer)
	router.Route("/tracks", func(r chi.Router) { r.Mount("/", tracks.Routes()) })
	return router
}

func buildPlaylistHandler(playlists *catalog.InMemoryPlaylistRepo, tracks *catalog.InMemoryTrackRepo) chi.Router {
	h := catalog.NewPlaylistHandler(
		catalog.NewPlaylistLifecycleService(playlists),
		catalog.NewPlaylistMembershipService(playlists, tracks),
	)
	router := chi.NewRouter()
	router.Use(asBearer)
	router.Mount("/playlists", h.Routes())
	return router
}

type writeRig struct {
	router    chi.Router
	tracks    *catalog.InMemoryTrackRepo
	playlists *catalog.InMemoryPlaylistRepo
}

func newWriteRig(trackOpts []func(*catalog.TrackHandler), playlistOpts []func(*catalog.PlaylistHandler)) *writeRig {
	tracks := catalog.NewInMemoryTrackRepo()
	playlists := catalog.NewInMemoryPlaylistRepo()
	playlistH := catalog.NewPlaylistHandler(
		catalog.NewPlaylistLifecycleService(playlists),
		catalog.NewPlaylistMembershipService(playlists, tracks),
		playlistOpts...,
	)
	r := trackRoutes(tracks, trackOpts...)
	r.Mount("/playlists", playlistH.Routes())
	return &writeRig{router: r, tracks: tracks, playlists: playlists}
}

func newThrottledWriteRig(now func() time.Time) *writeRig {
	return newWriteRig(
		[]func(*catalog.TrackHandler){catalog.WithTrackWriteLimiterClock(now)},
		[]func(*catalog.PlaylistHandler){catalog.WithPlaylistWriteLimiterClock(now)},
	)
}

func (rig *writeRig) post(t *testing.T, user shared.UserId, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	return sendAs(rig.router, jsonRequest(t, http.MethodPost, path, body), user)
}

func (rig *writeRig) createTrack(t *testing.T, user shared.UserId, title string) *httptest.ResponseRecorder {
	t.Helper()
	return rig.post(t, user, "/tracks/", map[string]string{"title": title, "artist": "Artist", "album": "Album"})
}

func (rig *writeRig) createPlaylist(t *testing.T, user shared.UserId, name string) *httptest.ResponseRecorder {
	t.Helper()
	return rig.post(t, user, "/playlists/", map[string]string{"name": name})
}

func (rig *writeRig) addTrackToPlaylist(t *testing.T, user shared.UserId, playlistId catalog.PlaylistId, trackId catalog.TrackId) *httptest.ResponseRecorder {
	t.Helper()
	return rig.post(t, user, "/playlists/"+playlistId.String()+"/tracks", map[string]string{"track_id": trackId.String()})
}

func (rig *writeRig) addTracksToPlaylist(t *testing.T, user shared.UserId, playlistId catalog.PlaylistId, trackId catalog.TrackId) *httptest.ResponseRecorder {
	t.Helper()
	return rig.post(t, user, "/playlists/"+playlistId.String()+"/tracks/batch", map[string][]string{"track_ids": {trackId.String()}})
}

func (rig *writeRig) seedPlaylistAndTrack(t *testing.T, user shared.UserId) (catalog.PlaylistId, catalog.TrackId) {
	t.Helper()
	playlist := catalog.SeedPlaylist(t, rig.playlists, user, "Seeded")
	track := catalog.SeedTrack(t, rig.tracks, user, "Seeded Track", "Artist", "Album")
	return playlist.ID, track.ID
}
