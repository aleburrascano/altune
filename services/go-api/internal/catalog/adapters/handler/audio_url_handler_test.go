package handler_test

import (
	"altune/go-api/internal/catalog"
	"altune/go-api/internal/shared/httputil"
	"altune/go-api/internal/shared/logging"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const audioURLBatchCap = 200

type resolveRequest struct {
	TrackIDs []string `json:"track_ids"`
}

func audioURLRoutes(store catalog.AudioStore, opts ...func(*catalog.AudioURLHandler)) (*catalog.InMemoryTrackRepo, chi.Router) {
	repo := catalog.NewInMemoryTrackRepo()
	router := chi.NewRouter()
	router.Use(asBearer)
	catalog.NewAudioURLHandler(catalog.NewAudioURLService(repo, store), opts...).Routes(router)
	return repo, router
}

func resolveOneReadyTrack(t *testing.T, opts ...func(*catalog.AudioURLHandler)) map[string]any {
	t.Helper()
	repo, router := audioURLRoutes(catalog.NewInMemoryAudioStore(), opts...)
	track := catalog.SeedReadyTrack(t, repo, catalog.FixtureUserId(), "Track", "Artist", "Album", "audio/ok.opus")

	rec := postJSON(t, router, "/audio-urls", resolveRequest{TrackIDs: []string{track.ID.UUID().String()}})

	assertStatus(t, rec, http.StatusOK)
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp
}

func TestAudioURLs_ReportPrefetchKillSwitch(t *testing.T) {
	if got := resolveOneReadyTrack(t)["prefetch_enabled"]; got != true {
		t.Errorf("default prefetch_enabled = %v, want true (preserve current behavior)", got)
	}
	if got := resolveOneReadyTrack(t, catalog.WithPrefetchEnabled(true))["prefetch_enabled"]; got != true {
		t.Errorf("enabled prefetch_enabled = %v, want true", got)
	}
	if got := resolveOneReadyTrack(t, catalog.WithPrefetchEnabled(false))["prefetch_enabled"]; got != false {
		t.Errorf("disabled prefetch_enabled = %v, want false so clients stop prefetching", got)
	}
}

func unknownTrackIDs(n int) []string {
	ids := make([]string, n)
	for i := range ids {
		ids[i] = uuid.NewString()
	}
	return ids
}

func TestAudioURLs_TheBatchCapIsTwoHundred(t *testing.T) {
	_, router := audioURLRoutes(catalog.NewInMemoryAudioStore())

	atCap := postJSON(t, router, "/audio-urls", resolveRequest{TrackIDs: unknownTrackIDs(audioURLBatchCap)})
	assertStatus(t, atCap, http.StatusOK)

	overCap := postJSON(t, router, "/audio-urls", resolveRequest{TrackIDs: unknownTrackIDs(audioURLBatchCap + 1)})
	assertStatus(t, overCap, http.StatusBadRequest)
	assertErrorCode(t, overCap, "catalog.batch_too_large")
}

func TestAudioURLs_RejectsAMalformedTrackIDWithItsOwnCode(t *testing.T) {
	_, router := audioURLRoutes(catalog.NewInMemoryAudioStore())

	rec := postJSON(t, router, "/audio-urls", resolveRequest{TrackIDs: []string{"not-a-uuid"}})

	assertStatus(t, rec, http.StatusBadRequest)
	assertErrorCode(t, rec, "catalog.invalid_track_id")
}

func TestAudioURLs_ResponseIsNeverCached(t *testing.T) {
	repo, router := audioURLRoutes(catalog.NewInMemoryAudioStore())
	track := catalog.SeedReadyTrack(t, repo, catalog.FixtureUserId(), "Track", "Artist", "Album", "audio/ok.opus")

	rec := postJSON(t, router, "/audio-urls", resolveRequest{TrackIDs: []string{track.ID.UUID().String()}})

	assertStatus(t, rec, http.StatusOK)
	assertPrivateAudioHeaders(t, rec, "private, no-store")
}

type failingSigner struct {
	*catalog.InMemoryAudioStore
}

func (failingSigner) PresignGet(context.Context, string, time.Duration) (string, error) {
	return "", errors.New("presign boom")
}

func TestCorrelationID_ReachesCatalogServiceLogs(t *testing.T) {
	prev := slog.Default()
	defer slog.SetDefault(prev)
	ring := logging.Setup("info", false)

	repo := catalog.NewInMemoryTrackRepo()
	track := catalog.SeedReadyTrack(t, repo, catalog.FixtureUserId(), "Track", "Artist", "Album", "audio/ok.opus")
	router := chi.NewRouter()
	router.Use(httputil.CorrelationID)
	router.Use(httputil.RequestLogger)
	router.Use(asBearer)
	catalog.NewAudioURLHandler(catalog.NewAudioURLService(repo, failingSigner{catalog.NewInMemoryAudioStore()})).Routes(router)

	const inbound = "trace-abc12345"
	req := jsonRequest(t, http.MethodPost, "/audio-urls", resolveRequest{TrackIDs: []string{track.ID.UUID().String()}})
	req.Header.Set("X-Correlation-ID", inbound)
	rec := sendAs(router, req, catalog.FixtureUserId())

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
