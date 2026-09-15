package handler

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/playback/domain"
	"altune/go-api/internal/playback/ports"
	"altune/go-api/internal/playback/service"
	"altune/go-api/internal/shared"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
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

func deleteReq() *http.Request {
	req := httptest.NewRequest(http.MethodDelete, "/queue-state", nil)
	ctx := auth.ContextWithUserID(req.Context(), shared.NewUserId(uuid.New()))
	return req.WithContext(ctx)
}

func TestHandleForget_DeleteRouteErasesPersistedState(t *testing.T) {
	// Reproduces #619: QueueService.Forget (the GDPR erasure entrypoint) had no
	// route wired, so an erasure request could not reach it in production. An
	// authenticated DELETE /queue-state must reach Forget and erase the state.
	repo := &recordingRepo{saved: &domain.QueueState{}}
	h := newHandler(repo)
	rec := httptest.NewRecorder()

	h.Routes().ServeHTTP(rec, deleteReq())

	if rec.Code != http.StatusNoContent {
		t.Fatalf("erasure must succeed with 204, got status %d body %q", rec.Code, rec.Body.String())
	}
	if repo.saved != nil {
		t.Fatalf("persisted queue state must be erased, still have %+v", repo.saved)
	}
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

type staleRepo struct {
	recordingRepo
}

func (r *staleRepo) Upsert(_ context.Context, _ *domain.QueueState) error {
	return fmt.Errorf("upsert: %w", domain.ErrStaleQueueWrite)
}

func TestHandleSave_RejectedStaleWriteIsConflictNot204(t *testing.T) {
	h := newHandler(&staleRepo{})
	rec := httptest.NewRecorder()

	h.Routes().ServeHTTP(rec, savePut(`{"track_ids":[],"repeat_mode":"off","source_id":"library"}`))

	if rec.Code != http.StatusConflict {
		t.Fatalf("a save the stale-write guard rejected must be a 409, got status %d body %q", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"playback.stale_queue_write"`) {
		t.Errorf("expected the stale-write error code in the body, got %q", rec.Body.String())
	}
}

func TestHandleSave_GarbageLegacySourceIdRejectedNotPersisted(t *testing.T) {
	// Reproduces #620: with only a garbage source_id and no structured source,
	// the save used to succeed (204) and persist the garbage, which then read
	// back as source: null while echoing the garbage in source_id. It must now
	// be rejected the same way an unknown structured kind is.
	repo := &recordingRepo{}
	h := newHandler(repo)
	rec := httptest.NewRecorder()

	h.Routes().ServeHTTP(rec, savePut(`{"track_ids":[],"repeat_mode":"off","source_id":"mixtape:7"}`))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("garbage legacy source_id must be a validation error, got status %d body %q", rec.Code, rec.Body.String())
	}
	if repo.saved != nil {
		t.Fatalf("nothing should be persisted for a rejected legacy source_id, got %+v", repo.saved)
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

type failingNowPlaying struct{}

func (failingNowPlaying) Lookup(_ context.Context, _ shared.UserId, _ string) (*ports.NowPlayingTrack, error) {
	return nil, errors.New("lookup now-playing track: context deadline exceeded")
}

// getResume serves GET /queue-state for a saved queue whose current index
// points at a track, so the handler always attempts now-playing enrichment.
func getResume(t *testing.T, nowPlaying ports.NowPlayingReader) (int, map[string]json.RawMessage) {
	t.Helper()
	repo := &recordingRepo{saved: &domain.QueueState{
		TrackIds:     []string{"t1", "t2"},
		CurrentIdx:   1,
		NaturalOrder: []string{"t1", "t2"},
	}}
	h := NewQueueHandler(service.NewQueueService(repo, nowPlaying))
	req := httptest.NewRequest(http.MethodGet, "/queue-state", nil)
	req = req.WithContext(auth.ContextWithUserID(req.Context(), shared.NewUserId(uuid.New())))
	rec := httptest.NewRecorder()

	h.Routes().ServeHTTP(rec, req)

	var body map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body %q: %v", rec.Body.String(), err)
	}
	return rec.Code, body
}

// Reproduces #1122: a now-playing lookup that failed on a dependency fault
// produced a body identical to "the current track is absent", so a client
// could not tell a transient enrichment outage from nothing playing.
func TestHandleGet_FailedNowPlayingLookupIsDistinguishableFromAbsentTrack(t *testing.T) {
	failedCode, failed := getResume(t, failingNowPlaying{})
	absentCode, absent := getResume(t, nilNowPlaying{})

	if failedCode != http.StatusOK || absentCode != http.StatusOK {
		t.Fatalf("resume must still succeed with 200 either way, got failed=%d absent=%d", failedCode, absentCode)
	}
	if v, ok := failed["current_track"]; ok {
		t.Errorf("a failed lookup must not invent a current_track, got %s", v)
	}
	if got := string(failed["current_index"]); got != "1" {
		t.Errorf("a failed lookup must still return the queue state, current_index=%q", got)
	}
	if got := string(failed["current_track_unavailable"]); got != "true" {
		t.Errorf("a failed lookup must set current_track_unavailable=true, got %q", got)
	}
	if v, ok := absent["current_track_unavailable"]; ok {
		t.Errorf("an absent track is not a failure; current_track_unavailable must be omitted, got %s", v)
	}
}
