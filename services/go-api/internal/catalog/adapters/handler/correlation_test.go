package handler

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/service"
	"altune/go-api/internal/shared/httputil"
	"altune/go-api/internal/shared/logging"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

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
