package handler_test

import (
	"altune/go-api/internal/library"
	"altune/go-api/internal/library/librarytest"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/requestuser"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
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
				r = r.WithContext(requestuser.ContextWithUserID(r.Context(), ids.NewUserId(id)))
			}
		}
		next.ServeHTTP(w, r)
	})
}

type trackDeps struct {
	scheduler *library.InMemoryScheduler
	backfill  *library.FillFeaturedArtistsService
}

func (d trackDeps) addScheduler() library.AcquisitionScheduler {
	if d.scheduler == nil {
		return &library.InMemoryScheduler{}
	}
	return d.scheduler
}

func (d trackDeps) backfillFor(repo *library.InMemoryTrackRepo) *library.FillFeaturedArtistsService {
	if d.backfill != nil {
		return d.backfill
	}
	return library.MustNewBackfillFeaturedService(repo, repo, library.InMemoryFeaturedResolver{}, time.Now)
}

func newRouter() chi.Router {
	router := chi.NewRouter()
	router.Use(asBearer)
	return router
}

func trackRoutes(repo *library.InMemoryTrackRepo, opts ...func(*library.TrackHandler)) chi.Router {
	return trackRoutesWith(repo, trackDeps{}, opts...)
}

func trackRoutesWith(repo *library.InMemoryTrackRepo, deps trackDeps, opts ...func(*library.TrackHandler)) chi.Router {
	router := newRouter()
	mountTracks(router, repo, deps, time.Now, opts...)
	return router
}

func mountTracks(router chi.Router, repo *library.InMemoryTrackRepo, deps trackDeps, now func() time.Time, opts ...func(*library.TrackHandler)) {
	tracks := library.MustNewTrackHandler(
		library.MustNewAddTrackService(repo, events.NoopPublisher(), deps.addScheduler(), time.Now),
		library.NewListTracksService(repo),
		library.NewGetTrackStatusService(repo),
		library.NewDeleteTrackService(repo, library.NewInMemoryAudioStore(), events.NoopPublisher(), &library.InMemoryAudioStoreMetrics{}, library.NewInMemoryOrphanedAudioQueue(library.NewInMemoryTrackRepo(), clock.Now)),
		library.NewSetTrackNumberService(repo),
		library.NewFeaturedArtistHandler(deps.backfillFor(repo), library.NewListFeaturingService(repo)), now,
		opts...)
	router.Route("/tracks", func(r chi.Router) { r.Mount("/", tracks.Routes()) })
}

func playlistRoutes(playlists *library.InMemoryPlaylistRepo, tracks *library.InMemoryTrackRepo, opts ...func(*library.PlaylistHandler)) chi.Router {
	router := newRouter()
	mountPlaylists(router, playlists, tracks, time.Now, opts...)
	return router
}

func mountPlaylists(router chi.Router, playlists *library.InMemoryPlaylistRepo, tracks *library.InMemoryTrackRepo, now func() time.Time, opts ...func(*library.PlaylistHandler)) {
	h := library.MustNewPlaylistHandler(
		library.MustNewPlaylistLifecycleService(playlists, events.NoopPublisher(), time.Now),
		library.MustNewPlaylistMembershipService(playlists, tracks, events.NoopPublisher(), time.Now), now,
		opts...)
	router.Mount("/playlists", h.Routes())
}

func request(t *testing.T, router http.Handler, method, path string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	return requestAs(t, router, librarytest.UserId(), method, path, body)
}

func requestAs(t *testing.T, router http.Handler, user ids.UserId, method, path string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, body)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return sendAs(router, req, user)
}

func newWriteRig(now func() time.Time) *writeRig {
	tracks := library.NewInMemoryTrackRepo()
	playlists := library.NewInMemoryPlaylistRepo()
	router := newRouter()
	mountTracks(router, tracks, trackDeps{}, now)
	mountPlaylists(router, playlists, tracks, now)
	return &writeRig{router: router, tracks: tracks, playlists: playlists}
}

func sendAs(router http.Handler, req *http.Request, user ids.UserId) *httptest.ResponseRecorder {
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

func assertProblem(t *testing.T, rec *httptest.ResponseRecorder, wantCode, wantDetail string) {
	t.Helper()
	var body struct {
		Code   string `json:"code"`
		Detail string `json:"detail"`
	}
	decodeJSON(t, rec, &body)
	if body.Code != wantCode || body.Detail != wantDetail {
		t.Errorf("problem = {%q, %q}, want {%q, %q}", body.Code, body.Detail, wantCode, wantDetail)
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
	tracks    *library.InMemoryTrackRepo
	playlists *library.InMemoryPlaylistRepo
}

func newThrottledWriteRig(now func() time.Time) *writeRig {
	return newWriteRig(now)
}

func (rig *writeRig) post(t *testing.T, user ids.UserId, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	return requestAs(t, rig.router, user, http.MethodPost, path, jsonBody(t, body))
}

func (rig *writeRig) createTrack(t *testing.T, user ids.UserId, title string) *httptest.ResponseRecorder {
	t.Helper()
	return rig.post(t, user, "/tracks/", map[string]string{"title": title, "artist": "Artist", "album": "Album"})
}

func (rig *writeRig) createPlaylist(t *testing.T, user ids.UserId, name string) *httptest.ResponseRecorder {
	t.Helper()
	return rig.post(t, user, "/playlists/", map[string]string{"name": name})
}

func (rig *writeRig) addTrackToPlaylist(t *testing.T, user ids.UserId, playlistId library.PlaylistId, trackId library.TrackId) *httptest.ResponseRecorder {
	t.Helper()
	return rig.post(t, user, "/playlists/"+playlistId.String()+"/tracks", map[string]string{"track_id": trackId.String()})
}

func (rig *writeRig) addTracksToPlaylist(t *testing.T, user ids.UserId, playlistId library.PlaylistId, trackId library.TrackId) *httptest.ResponseRecorder {
	t.Helper()
	return rig.post(t, user, "/playlists/"+playlistId.String()+"/tracks/batch", map[string][]string{"track_ids": {trackId.String()}})
}

func (rig *writeRig) seedPlaylistAndTrack(t *testing.T, user ids.UserId) (library.PlaylistId, library.TrackId) {
	t.Helper()
	playlist := librarytest.SeedPlaylist(t, rig.playlists, user, "Seeded")
	track := librarytest.SeedTrack(t, rig.tracks, user, "Seeded Track", "Artist", "Album")
	return playlist.ID, track.ID
}

func postgresTrackRoutes(pool *pgxpool.Pool) chi.Router {
	repo := library.NewPostgresCatalogTrackRepository(pool)
	tracks := library.MustNewTrackHandler(
		library.MustNewAddTrackService(repo, events.NoopPublisher(), &library.InMemoryScheduler{}, time.Now),
		library.NewListTracksService(repo),
		library.NewGetTrackStatusService(repo),
		library.NewDeleteTrackService(repo, library.NewInMemoryAudioStore(), events.NoopPublisher(), &library.InMemoryAudioStoreMetrics{}, library.NewInMemoryOrphanedAudioQueue(library.NewInMemoryTrackRepo(), clock.Now)),
		library.NewSetTrackNumberService(repo),
		library.NewFeaturedArtistHandler(
			library.MustNewBackfillFeaturedService(repo, repo, library.InMemoryFeaturedResolver{}, time.Now),
			library.NewListFeaturingService(repo),
		), time.Now)
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

type failingTrackRepo struct {
	*library.InMemoryTrackRepo
	err error
}

func (r *failingTrackRepo) Add(context.Context, *library.Track) (*library.Track, bool, error) {
	return nil, false, r.err
}

func (r *failingTrackRepo) ListFilteredForUser(context.Context, ids.UserId, library.LibraryQuery) ([]*library.Track, int, error) {
	return nil, 0, r.err
}

func (r *failingTrackRepo) ListByIDs(context.Context, ids.UserId, []library.TrackId) ([]*library.Track, error) {
	return nil, r.err
}

func (r *failingTrackRepo) SetTrackNumber(context.Context, library.TrackId, ids.UserId, int) (bool, error) {
	return false, r.err
}

func (r *failingTrackRepo) Delete(context.Context, library.TrackId, ids.UserId) (bool, *string, error) {
	return false, nil, r.err
}

func failingRepoRoutes(err error) chi.Router {
	repo := &failingTrackRepo{InMemoryTrackRepo: library.NewInMemoryTrackRepo(), err: err}
	tracks := library.MustNewTrackHandler(
		library.MustNewAddTrackService(repo, events.NoopPublisher(), &library.InMemoryScheduler{}, time.Now),
		library.NewListTracksService(repo),
		library.NewGetTrackStatusService(repo),
		library.NewDeleteTrackService(repo, library.NewInMemoryAudioStore(), events.NoopPublisher(), &library.InMemoryAudioStoreMetrics{}, library.NewInMemoryOrphanedAudioQueue(library.NewInMemoryTrackRepo(), clock.Now)),
		library.NewSetTrackNumberService(repo),
		library.NewFeaturedArtistHandler(
			library.MustNewBackfillFeaturedService(repo, repo, library.InMemoryFeaturedResolver{}, time.Now),
			library.NewListFeaturingService(repo),
		), time.Now)
	router := newRouter()
	router.Route("/tracks", func(r chi.Router) { r.Mount("/", tracks.Routes()) })
	signer := &countingAudioStore{InMemoryAudioStore: library.NewInMemoryAudioStore()}
	library.MustNewAudioURLHandler(library.MustNewAudioURLService(repo, signer, time.Now, &librarytest.Metrics{}), time.Now).Routes(router)
	return router
}
