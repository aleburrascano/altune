package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"

	"altune/go-api/internal/auth"
	"altune/go-api/internal/catalog/catalogtest"
	catdomain "altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/catalog/service"
	"altune/go-api/internal/shared"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// --- test constants ---

var (
	testUserUUID = uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	testUserId   = shared.NewUserId(testUserUUID)
)

// --- fake token verifier ---

// verifyAsTestUser always succeeds and returns testUserId.
var verifyAsTestUser = auth.VerifierFunc(func(context.Context, string) (shared.UserId, error) {
	return testUserId, nil
})

// serve sends a request through a chi router and returns the response recorder.
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

// serveNoAuth sends a request without an Authorization header.
func serveNoAuth(t *testing.T, router chi.Router, method, path string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, body)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// jsonBody encodes v as JSON for use as a request body.
func jsonBody(t *testing.T, v any) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	if err := json.NewEncoder(buf).Encode(v); err != nil {
		t.Fatalf("jsonBody: %v", err)
	}
	return buf
}

// decodeJSON decodes the response body into dst.
func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder, dst any) {
	t.Helper()
	if err := json.NewDecoder(rec.Body).Decode(dst); err != nil {
		t.Fatalf("decodeJSON: %v (body: %s)", err, rec.Body.String())
	}
}

// --- domain helpers ---

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
	p, _ := catdomain.NewPlaylist(userId, name)
	return p
}

// --- service builders ---

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
	backfillSvc := service.NewBackfillFeaturedService(trackRepo, nil)
	listFeaturingSvc := service.NewListFeaturingService(trackRepo)
	h := NewTrackHandler(addSvc, listSvc, getStatusSvc, deleteSvc, setTrackNumberSvc)
	featuredH := NewFeaturedArtistHandler(backfillSvc, listFeaturingSvc)
	r := chi.NewRouter()
	r.Use(auth.Middleware(verifyAsTestUser))
	r.Route("/tracks", func(r chi.Router) {
		r.Mount("/", h.Routes())
		featuredH.AddRoutes(r)
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

// assertStatus checks the HTTP status code.
func assertStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Errorf("status = %d, want %d (body: %s)", rec.Code, want, rec.Body.String())
	}
}

// assertJSON checks that the response has application/json content type.
func assertJSON(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" && ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}
