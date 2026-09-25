package handler

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/catalog/service"
	"altune/go-api/internal/shared"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

var verifyAsTestUser = auth.VerifierFunc(func(context.Context, string) (auth.VerifiedToken, error) {
	return auth.VerifiedToken{UserID: testUserId, ExpiresAt: time.Now().Add(time.Hour)}, nil
})

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

func makeReadyTrack(userId shared.UserId, title, artist, album, audioRef string) *catdomain.Track {
	t := makeTrack(userId, title, artist, album)
	_ = t.MarkReady(audioRef)
	return t
}

func makePlaylist(userId shared.UserId, name string) *catdomain.Playlist {
	p, _ := catdomain.NewPlaylist(userId, name, time.Now())
	return p
}

// fakeResolver finds nothing: the handler tests cover the backfill route's
// admission and response, never a provider lookup.
type fakeResolver struct{}

func (fakeResolver) Resolve(_ context.Context, _, _ string) ([]catdomain.FeaturedArtist, error) {
	return nil, nil
}

func buildTrackHandler(trackRepo *catalogtest.TrackRepo, scheduler *catalogtest.Scheduler) (*TrackHandler, chi.Router) {
	var addOpts []func(*service.AddTrackService)
	if scheduler != nil {
		addOpts = append(addOpts, service.WithAcquisitionScheduler(scheduler))
	}
	addSvc := service.NewAddTrackService(trackRepo, addOpts...)
	listSvc := service.NewListTracksService(trackRepo)
	deleteSvc := service.NewDeleteTrackService(trackRepo, catalogtest.NewAudioStore())
	setTrackNumberSvc := service.NewSetTrackNumberService(trackRepo)

	getStatusSvc := service.NewGetTrackStatusService(trackRepo)
	backfillSvc := service.NewBackfillFeaturedService(trackRepo, trackRepo, fakeResolver{})
	listFeaturingSvc := service.NewListFeaturingService(trackRepo)
	featuredH := NewFeaturedArtistHandler(backfillSvc, listFeaturingSvc)
	h := NewTrackHandler(addSvc, listSvc, getStatusSvc, deleteSvc, setTrackNumberSvc, featuredH)
	r := chi.NewRouter()
	r.Use(auth.Middleware(verifyAsTestUser))
	r.Route("/tracks", func(r chi.Router) {
		r.Mount("/", h.Routes())
	})
	return h, r
}

func buildPlaylistHandler(plRepo *catalogtest.PlaylistRepo, trRepo *catalogtest.TrackRepo) (*PlaylistHandler, chi.Router) {
	lifecycleSvc := service.NewPlaylistLifecycleService(plRepo)
	membershipSvc := service.NewPlaylistMembershipService(plRepo, trRepo)
	h := NewPlaylistHandler(lifecycleSvc, membershipSvc)
	r := chi.NewRouter()
	r.Use(auth.Middleware(verifyAsTestUser))
	r.Mount("/playlists", h.Routes())
	return h, r
}

func buildStreamHandler(trackRepo *catalogtest.TrackRepo, audioStore *catalogtest.AudioStore, scheduler *catalogtest.Scheduler) (*StreamHandler, chi.Router) {
	var sched ports.AcquisitionScheduler
	if scheduler != nil {
		sched = scheduler
	}
	streamSvc := service.NewStreamTrackService(trackRepo, audioStore, service.WithStreamScheduler(sched))
	h := NewStreamHandler(streamSvc)
	r := chi.NewRouter()
	r.Use(auth.Middleware(verifyAsTestUser))
	r.Get("/tracks/{trackId}/stream", h.HandleStreamAudio)
	return h, r
}

func assertStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Errorf("status = %d, want %d (body: %s)", rec.Code, want, rec.Body.String())
	}
}

// assertErrorCode reads the machine-readable code off an error response: the
// code is the contract clients branch on, the detail is prose that may change.
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

func (rig *writeRig) addTrackToPlaylist(user shared.UserId, playlistId catdomain.PlaylistId, trackId catdomain.TrackId) *httptest.ResponseRecorder {
	return rig.post(user, "/playlists/"+playlistId.String()+"/tracks",
		fmt.Sprintf(`{"track_id":%q}`, trackId.String()))
}

func (rig *writeRig) addTracksToPlaylist(user shared.UserId, playlistId catdomain.PlaylistId, trackId catdomain.TrackId) *httptest.ResponseRecorder {
	return rig.post(user, "/playlists/"+playlistId.String()+"/tracks/batch",
		fmt.Sprintf(`{"track_ids":[%q]}`, trackId.String()))
}

// seedPlaylistAndTrack gives user an owned playlist and an owned track, so a
// membership add is refused by the throttle alone and never by ownership.
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
