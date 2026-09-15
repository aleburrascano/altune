package handler

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/service"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
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
