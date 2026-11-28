package handler_test

import (
	"altune/go-api/internal/library/internal/adapters/handler"
	"altune/go-api/internal/library/internal/adapters/memory"
	"altune/go-api/internal/library/internal/adapters/persistence"
	"altune/go-api/internal/library/internal/adapters/storage"
	"altune/go-api/internal/library/internal/ports"
	"altune/go-api/internal/library/internal/service"
	"altune/go-api/internal/library/librarytest"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/httpserver"
	"altune/go-api/internal/shared/logging"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const audioURLBatchCap = 200

type resolveRequest struct {
	TrackIDs []string `json:"track_ids"`
}

func audioURLRoutes(store ports.AudioLinkSigner, opts ...func(*handler.AudioURLHandler)) (*memory.TrackRepo, chi.Router) {
	repo := memory.NewTrackRepo()
	router := chi.NewRouter()
	router.Use(asBearer)
	handler.MustNewAudioURLHandler(service.MustNewAudioURLService(repo, store, time.Now, &librarytest.Metrics{}), time.Now, opts...).Routes(router)
	return repo, router
}

func resolveOneReadyTrack(t *testing.T, opts ...func(*handler.AudioURLHandler)) map[string]any {
	t.Helper()
	repo, router := audioURLRoutes(storage.NoAudioLinks{}, opts...)
	track := librarytest.SeedReadyTrack(t, repo, librarytest.UserId(), "Track", "Artist", "Album", "audio/ok.opus")

	rec := request(t, router, http.MethodPost, "/audio-urls", jsonBody(t, resolveRequest{TrackIDs: []string{track.ID().UUID().String()}}))

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
	if got := resolveOneReadyTrack(t, handler.WithPrefetchEnabled(true))["prefetch_enabled"]; got != true {
		t.Errorf("enabled prefetch_enabled = %v, want true", got)
	}
	if got := resolveOneReadyTrack(t, handler.WithPrefetchEnabled(false))["prefetch_enabled"]; got != false {
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
	_, router := audioURLRoutes(storage.NoAudioLinks{})

	atCap := request(t, router, http.MethodPost, "/audio-urls", jsonBody(t, resolveRequest{TrackIDs: unknownTrackIDs(audioURLBatchCap)}))
	assertStatus(t, atCap, http.StatusOK)

	overCap := request(t, router, http.MethodPost, "/audio-urls", jsonBody(t, resolveRequest{TrackIDs: unknownTrackIDs(audioURLBatchCap + 1)}))
	assertStatus(t, overCap, http.StatusBadRequest)
	assertErrorCode(t, overCap, "catalog.batch_too_large")
}

func TestAudioURLs_RejectsAMalformedTrackIDWithItsOwnCode(t *testing.T) {
	_, router := audioURLRoutes(storage.NoAudioLinks{})

	rec := request(t, router, http.MethodPost, "/audio-urls", jsonBody(t, resolveRequest{TrackIDs: []string{"not-a-uuid"}}))

	assertStatus(t, rec, http.StatusBadRequest)
	assertErrorCode(t, rec, "catalog.invalid_track_id")
}

func TestAudioURLs_ResponseIsNeverCached(t *testing.T) {
	repo, router := audioURLRoutes(storage.NoAudioLinks{})
	track := librarytest.SeedReadyTrack(t, repo, librarytest.UserId(), "Track", "Artist", "Album", "audio/ok.opus")

	rec := request(t, router, http.MethodPost, "/audio-urls", jsonBody(t, resolveRequest{TrackIDs: []string{track.ID().UUID().String()}}))

	assertStatus(t, rec, http.StatusOK)
	assertAudioCacheControl(t, rec, "private, no-store")
}

type failingSigner struct {
	*memory.AudioStore
}

func (failingSigner) SignsLinks() bool { return true }

func (failingSigner) PresignGet(context.Context, string, time.Duration) (string, error) {
	return "", errors.New("presign boom")
}

func TestCorrelationID_ReachesCatalogServiceLogs(t *testing.T) {
	ring := logging.CaptureRing(t, "info")

	repo := memory.NewTrackRepo()
	track := librarytest.SeedReadyTrack(t, repo, librarytest.UserId(), "Track", "Artist", "Album", "audio/ok.opus")
	router := chi.NewRouter()
	router.Use(httpserver.CorrelationID)
	router.Use(httpserver.RequestLogger(clock.Now))
	router.Use(asBearer)
	handler.MustNewAudioURLHandler(service.MustNewAudioURLService(repo, failingSigner{memory.NewAudioStore()}, time.Now, &librarytest.Metrics{}), time.Now).Routes(router)

	const inbound = "trace-abc12345"
	req := httptest.NewRequest(http.MethodPost, "/audio-urls", jsonBody(t, resolveRequest{TrackIDs: []string{track.ID().UUID().String()}}))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Correlation-ID", inbound)
	rec := sendAs(router, req, librarytest.UserId())

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

func TestAudioURLs_ARefusedCatalogDatabaseAnswersTemporarilyUnavailable(t *testing.T) {
	router := chi.NewRouter()
	router.Use(asBearer)
	repo := persistence.NewPostgresTrackRepository(refusedPool(t), librarytest.DiscardDBCallMetrics())
	signer := &countingAudioStore{AudioStore: memory.NewAudioStore()}
	handler.MustNewAudioURLHandler(service.MustNewAudioURLService(repo, signer, time.Now, &librarytest.Metrics{}), time.Now).Routes(router)

	rec := request(t, router, http.MethodPost, "/audio-urls", jsonBody(t, resolveRequest{TrackIDs: []string{uuid.NewString()}}))

	assertCatalogTemporarilyUnavailable(t, rec)
}
