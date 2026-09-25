package handler

import (
	"altune/go-api/internal/playback/domain"
	"altune/go-api/internal/playback/ports"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type unavailableRepo struct {
	recordingRepo
}

func (unavailableRepo) Upsert(_ context.Context, _ *domain.QueueState) error {
	return fmt.Errorf("upsert: %w", ports.ErrQueueStateUnavailable)
}

func TestHandleSave_TransientFailureIsRetryable503(t *testing.T) {
	rec := httptest.NewRecorder()

	newHandler(&unavailableRepo{}).Routes().ServeHTTP(rec, savePut(`{"track_ids":[],"repeat_mode":"off","source_id":"library"}`))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503, body %q", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("missing Retry-After header")
	}
	if !strings.Contains(rec.Body.String(), `"code":"playback.unavailable"`) {
		t.Errorf("body = %q, want code playback.unavailable", rec.Body.String())
	}
}
