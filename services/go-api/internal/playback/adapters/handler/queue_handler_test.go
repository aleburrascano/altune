package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"altune/go-api/internal/auth"
	"altune/go-api/internal/playback/domain"
	"altune/go-api/internal/playback/ports"
	"altune/go-api/internal/playback/service"
	"altune/go-api/internal/shared"
)

type recordingRepo struct {
	saved *domain.QueueState
}

func (r *recordingRepo) Upsert(_ context.Context, state *domain.QueueState) error {
	r.saved = state
	return nil
}

func (r *recordingRepo) GetForUser(_ context.Context, _ shared.UserId) (*domain.QueueState, error) {
	return r.saved, nil
}

func (r *recordingRepo) DeleteForUser(_ context.Context, _ shared.UserId) error {
	r.saved = nil
	return nil
}

type nilNowPlaying struct{}

func (nilNowPlaying) Lookup(_ context.Context, _ shared.UserId, _ string) (*ports.NowPlayingTrack, error) {
	return nil, nil
}

func newHandler(repo ports.QueueStateRepository) *QueueHandler {
	return NewQueueHandler(service.NewQueueService(repo, nilNowPlaying{}))
}

func savePut(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPut, "/queue-state", strings.NewReader(body))
	ctx := auth.ContextWithUserID(req.Context(), shared.NewUserId(uuid.New()))
	return req.WithContext(ctx)
}

func TestHandleSave_UnknownKindRejectedNotEmpty204(t *testing.T) {
	repo := &recordingRepo{}
	h := newHandler(repo)
	rec := httptest.NewRecorder()

	h.Routes().ServeHTTP(rec, savePut(`{"track_ids":[],"repeat_mode":"off","source":{"kind":"album","playlist_id":"xyz"},"source_id":"album:xyz"}`))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown kind must be a validation error, got status %d body %q", rec.Code, rec.Body.String())
	}
	if repo.saved != nil {
		t.Fatalf("nothing should be persisted for a rejected source, got %+v", repo.saved)
	}
}

func TestHandleSave_KnownSourcePersists(t *testing.T) {
	repo := &recordingRepo{}
	h := newHandler(repo)
	rec := httptest.NewRecorder()

	h.Routes().ServeHTTP(rec, savePut(`{"track_ids":[],"repeat_mode":"off","source":{"kind":"playlist","playlist_id":"a:b","name":"x"}}`))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("valid source must be saved, got status %d body %q", rec.Code, rec.Body.String())
	}
	if repo.saved == nil {
		t.Fatal("expected state to be persisted")
	}
	if got := domain.ParseQueueSource(repo.saved.SourceId); got.PlaylistId != "a:b" || got.Name != "x" {
		t.Errorf("stored source_id lost data: %q -> %+v", repo.saved.SourceId, got)
	}
}

func TestHandleSave_LegacySourceIdPassesThrough(t *testing.T) {
	repo := &recordingRepo{}
	h := newHandler(repo)
	rec := httptest.NewRecorder()

	h.Routes().ServeHTTP(rec, savePut(`{"track_ids":[],"repeat_mode":"off","source_id":"library"}`))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("legacy source_id must be saved, got status %d body %q", rec.Code, rec.Body.String())
	}
	if repo.saved == nil || repo.saved.SourceId != "library" {
		t.Errorf("legacy source_id not preserved: %+v", repo.saved)
	}
}
