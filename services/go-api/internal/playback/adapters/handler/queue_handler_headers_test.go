package handler

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/playback/domain"
	"altune/go-api/internal/playback/service"
	"altune/go-api/internal/shared"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func TestHandleGet_ResponseIsUncachedAndNotSniffable(t *testing.T) {
	h := NewQueueHandler(service.NewQueueService(&recordingRepo{saved: &domain.QueueState{
		TrackIds:     []string{"t1"},
		NaturalOrder: []string{"t1"},
	}}, nilNowPlaying{}))
	req := httptest.NewRequest(http.MethodGet, "/queue-state", nil)
	req = req.WithContext(auth.ContextWithUserID(req.Context(), shared.NewUserId(uuid.New())))
	rec := httptest.NewRecorder()

	h.Routes().ServeHTTP(rec, req)

	if got := rec.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Errorf("Cache-Control = %q, want %q", got, "private, no-store")
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
}
