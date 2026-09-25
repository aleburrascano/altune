package handler

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/service"
	"altune/go-api/internal/shared/httputil"
	"altune/go-api/internal/shared/logging"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// resolvePrefetchFlag posts one audio-url resolve through a handler built with
// opts and returns the prefetch_enabled flag the response carried.
func resolvePrefetchFlag(t *testing.T, opts ...func(*AudioURLHandler)) any {
	t.Helper()
	repo := catalogtest.NewTrackRepo()
	track := makeReadyTrack(testUserId, "Track", "Artist", "Album", "audio/ok.opus")
	repo.Seed(track)
	svc := service.NewAudioURLService(repo, catalogtest.NewAudioStore())

	router := chi.NewRouter()
	router.Use(auth.Middleware(verifyAsTestUser))
	router.Post("/audio-urls", NewAudioURLHandler(svc, opts...).HandleResolve)

	body := jsonBody(t, resolveAudioURLsRequest{TrackIDs: []string{track.ID.UUID().String()}})
	req := httptest.NewRequest(http.MethodPost, "/audio-urls", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer fake-token")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusOK)
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp["prefetch_enabled"]
}

// TestAudioURLs_ReportPrefetchKillSwitch guards the remote lever behind
// AUDIO_PREFETCH_ENABLED: clients read prefetch_enabled from every resolve and
// stop prefetching audio to disk while it is false.
func TestAudioURLs_ReportPrefetchKillSwitch(t *testing.T) {
	if got := resolvePrefetchFlag(t); got != true {
		t.Errorf("default prefetch_enabled = %v, want true (preserve current behavior)", got)
	}
	if got := resolvePrefetchFlag(t, WithPrefetchEnabled(true)); got != true {
		t.Errorf("enabled prefetch_enabled = %v, want true", got)
	}
	if got := resolvePrefetchFlag(t, WithPrefetchEnabled(false)); got != false {
		t.Errorf("disabled prefetch_enabled = %v, want false so clients stop prefetching", got)
	}
}

// audioURLRouter serves the real /audio-urls route over an empty library: the
// request-shape rejections below never reach a track.
func audioURLRouter() chi.Router {
	svc := service.NewAudioURLService(catalogtest.NewTrackRepo(), catalogtest.NewAudioStore())
	router := chi.NewRouter()
	router.Use(auth.Middleware(verifyAsTestUser))
	NewAudioURLHandler(svc).Routes(router)
	return router
}

// A batch over the cap is a request the worker should split; a malformed id is
// one it should drop. Both are 400s, so the code is the only thing that tells
// them apart.
func TestAudioURLs_RejectsAnOversizedBatchWithItsOwnCode(t *testing.T) {
	ids := make([]string, maxAudioURLBatch+1)
	for i := range ids {
		ids[i] = uuid.NewString()
	}

	body := jsonBody(t, resolveAudioURLsRequest{TrackIDs: ids})
	rec := serve(t, audioURLRouter(), http.MethodPost, "/audio-urls", body)

	assertStatus(t, rec, http.StatusBadRequest)
	assertErrorCode(t, rec, "catalog.batch_too_large")
}

func TestAudioURLs_RejectsAMalformedTrackIDWithItsOwnCode(t *testing.T) {
	body := jsonBody(t, resolveAudioURLsRequest{TrackIDs: []string{"not-a-uuid"}})
	rec := serve(t, audioURLRouter(), http.MethodPost, "/audio-urls", body)

	assertStatus(t, rec, http.StatusBadRequest)
	assertErrorCode(t, rec, "catalog.invalid_track_id")
}

// TestAudioURLs_ResponseIsNeverCached pins #2199: the body is a list of
// presigned bearer URLs to one user's audio, live for up to an hour, so no
// cache between here and the client may keep it.
func TestAudioURLs_ResponseIsNeverCached(t *testing.T) {
	repo := catalogtest.NewTrackRepo()
	track := makeReadyTrack(testUserId, "Track", "Artist", "Album", "audio/ok.opus")
	repo.Seed(track)
	svc := service.NewAudioURLService(repo, catalogtest.NewAudioStore())

	router := chi.NewRouter()
	router.Use(auth.Middleware(verifyAsTestUser))
	NewAudioURLHandler(svc).Routes(router)

	body := jsonBody(t, resolveAudioURLsRequest{TrackIDs: []string{track.ID.UUID().String()}})
	rec := serve(t, router, http.MethodPost, "/audio-urls", body)

	assertStatus(t, rec, http.StatusOK)
	assertPrivateAudioHeaders(t, rec, "private, no-store")
}

// failingSigner is an audio store that always fails to presign, so the
// catalog service emits its own audio_url.presign_failed log line.
type failingSigner struct {
	*catalogtest.AudioStore
}

func (failingSigner) PresignGet(context.Context, string, time.Duration) (string, error) {
	return "", errors.New("presign boom")
}

// TestCorrelationID_ReachesCatalogServiceLogs is the bug's acceptance test:
// the correlation ID echoed in the response header must also appear in
// catalog's own deep log lines for that request, so a client-reported
// failure can be found by grepping logs for the ID it was given.
func TestCorrelationID_ReachesCatalogServiceLogs(t *testing.T) {
	prev := slog.Default()
	defer slog.SetDefault(prev)
	ring := logging.Setup("info", false)

	repo := catalogtest.NewTrackRepo()
	track := makeReadyTrack(testUserId, "Track", "Artist", "Album", "audio/ok.opus")
	repo.Seed(track)
	svc := service.NewAudioURLService(repo, failingSigner{catalogtest.NewAudioStore()})

	router := chi.NewRouter()
	router.Use(httputil.CorrelationID)
	router.Use(httputil.RequestLogger)
	router.Use(auth.Middleware(verifyAsTestUser))
	router.Post("/audio-urls", NewAudioURLHandler(svc).HandleResolve)

	const inbound = "trace-abc12345"
	body := jsonBody(t, resolveAudioURLsRequest{TrackIDs: []string{track.ID.UUID().String()}})
	req := httptest.NewRequest(http.MethodPost, "/audio-urls", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer fake-token")
	req.Header.Set("X-Correlation-ID", inbound)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusOK)
	echoed := rec.Header().Get("X-Correlation-ID")
	if echoed != inbound {
		t.Fatalf("response header X-Correlation-ID = %q, want inbound %q", echoed, inbound)
	}

	sawCatalogLog := false
	for _, r := range ring.Snapshot() {
		if r.Message != "audio_url.presign_failed" {
			continue
		}
		sawCatalogLog = true
		if r.Attrs["corr_id"] != echoed {
			t.Errorf("catalog log %q corr_id = %q, want echoed header %q", r.Message, r.Attrs["corr_id"], echoed)
		}
	}
	if !sawCatalogLog {
		t.Fatal("expected catalog's own audio_url.presign_failed log line, so the corr_id assertion is meaningful")
	}
}
