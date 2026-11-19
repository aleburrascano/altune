package handler_test

import (
	"altune/go-api/internal/catalog"
	shared "altune/go-api/internal/shared/kernel"
	"altune/go-api/internal/shared/requestuser"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
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

type trackDeps struct {
	scheduler *catalog.InMemoryScheduler
	backfill  *catalog.BackfillFeaturedService
}

func (d trackDeps) addOptions() []func(*catalog.AddTrackService) {
	if d.scheduler == nil {
		return nil
	}
	return []func(*catalog.AddTrackService){catalog.WithAcquisitionScheduler(d.scheduler)}
}

func (d trackDeps) backfillFor(repo *catalog.InMemoryTrackRepo) *catalog.BackfillFeaturedService {
	if d.backfill != nil {
		return d.backfill
	}
	return catalog.NewBackfillFeaturedService(repo, repo, catalog.InMemoryFeaturedResolver{})
}

func newRouter() chi.Router {
	router := chi.NewRouter()
	router.Use(asBearer)
	return router
}

func trackRoutes(repo *catalog.InMemoryTrackRepo, opts ...func(*catalog.TrackHandler)) chi.Router {
	return trackRoutesWith(repo, trackDeps{}, opts...)
}

func trackRoutesWith(repo *catalog.InMemoryTrackRepo, deps trackDeps, opts ...func(*catalog.TrackHandler)) chi.Router {
	router := newRouter()
	mountTracks(router, repo, deps, opts...)
	return router
}

func mountTracks(router chi.Router, repo *catalog.InMemoryTrackRepo, deps trackDeps, opts ...func(*catalog.TrackHandler)) {
	tracks := catalog.NewTrackHandler(
		catalog.NewAddTrackService(repo, deps.addOptions()...),
		catalog.NewListTracksService(repo),
		catalog.NewGetTrackStatusService(repo),
		catalog.NewDeleteTrackService(repo, catalog.NewInMemoryAudioStore()),
		catalog.NewSetTrackNumberService(repo),
		catalog.NewFeaturedArtistHandler(deps.backfillFor(repo), catalog.NewListFeaturingService(repo)),
		opts...,
	)
	router.Route("/tracks", func(r chi.Router) { r.Mount("/", tracks.Routes()) })
}

func playlistRoutes(playlists *catalog.InMemoryPlaylistRepo, tracks *catalog.InMemoryTrackRepo, opts ...func(*catalog.PlaylistHandler)) chi.Router {
	router := newRouter()
	mountPlaylists(router, playlists, tracks, opts...)
	return router
}

func mountPlaylists(router chi.Router, playlists *catalog.InMemoryPlaylistRepo, tracks *catalog.InMemoryTrackRepo, opts ...func(*catalog.PlaylistHandler)) {
	h := catalog.NewPlaylistHandler(
		catalog.NewPlaylistLifecycleService(playlists),
		catalog.NewPlaylistMembershipService(playlists, tracks),
		opts...,
	)
	router.Mount("/playlists", h.Routes())
}

func request(t *testing.T, router http.Handler, method, path string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	return requestAs(t, router, catalog.FixtureUserId(), method, path, body)
}

func requestAs(t *testing.T, router http.Handler, user shared.UserId, method, path string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, body)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return sendAs(router, req, user)
}

func requestAnonymous(t *testing.T, router http.Handler, method, path string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	return sendAnonymous(router, httptest.NewRequest(method, path, body))
}

func newWriteRig(trackOpts []func(*catalog.TrackHandler), playlistOpts []func(*catalog.PlaylistHandler)) *writeRig {
	tracks := catalog.NewInMemoryTrackRepo()
	playlists := catalog.NewInMemoryPlaylistRepo()
	router := newRouter()
	mountTracks(router, tracks, trackDeps{}, trackOpts...)
	mountPlaylists(router, playlists, tracks, playlistOpts...)
	return &writeRig{router: router, tracks: tracks, playlists: playlists}
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

type writeRig struct {
	router    chi.Router
	tracks    *catalog.InMemoryTrackRepo
	playlists *catalog.InMemoryPlaylistRepo
}

func newThrottledWriteRig(now func() time.Time) *writeRig {
	return newWriteRig(
		[]func(*catalog.TrackHandler){catalog.WithTrackWriteLimiterClock(now)},
		[]func(*catalog.PlaylistHandler){catalog.WithPlaylistWriteLimiterClock(now)},
	)
}

func (rig *writeRig) post(t *testing.T, user shared.UserId, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	return requestAs(t, rig.router, user, http.MethodPost, path, jsonBody(t, body))
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

func pgxTrackRoutes(pool *pgxpool.Pool) chi.Router {
	repo := catalog.NewPgxCatalogTrackRepository(pool)
	tracks := catalog.NewTrackHandler(
		catalog.NewAddTrackService(repo),
		catalog.NewListTracksService(repo),
		catalog.NewGetTrackStatusService(repo),
		catalog.NewDeleteTrackService(repo, catalog.NewInMemoryAudioStore()),
		catalog.NewSetTrackNumberService(repo),
		catalog.NewFeaturedArtistHandler(
			catalog.NewBackfillFeaturedService(repo, repo, catalog.InMemoryFeaturedResolver{}),
			catalog.NewListFeaturingService(repo),
		),
	)
	router := newRouter()
	router.Route("/tracks", func(r chi.Router) { r.Mount("/", tracks.Routes()) })
	return router
}

func refusedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return poolAt(t, "postgres://altune:secret@"+addr+"/altune?sslmode=disable")
}

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}
	return poolAt(t, dbURL)
}

func poolAt(t *testing.T, dbURL string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func assertCatalogTemporarilyUnavailable(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	assertStatus(t, rec, http.StatusServiceUnavailable)
	assertErrorCode(t, rec, "catalog.temporarily_unavailable")
}
