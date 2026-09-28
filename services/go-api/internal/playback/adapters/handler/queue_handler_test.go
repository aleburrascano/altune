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
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type recordingRepo struct {
	saved       *domain.QueueState
	upserts     int
	position    *domain.QueuePosition
	positionErr error
}

func (r *recordingRepo) Upsert(_ context.Context, state *domain.QueueState) error {
	r.saved = state
	r.upserts++
	return nil
}

func (r *recordingRepo) UpdatePosition(_ context.Context, position *domain.QueuePosition) error {
	r.position = position
	return r.positionErr
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

var clientPlaylistIdFormat = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func clientPlaylistId(storedId string) string {
	if clientPlaylistIdFormat.MatchString(storedId) {
		return storedId
	}
	return ""
}

func resavedSource(t *testing.T, resumed json.RawMessage) string {
	t.Helper()
	var source *queueSourceDTO
	if err := json.Unmarshal(resumed, &source); err != nil {
		t.Fatalf("decode source %s: %v", resumed, err)
	}
	if source == nil || source.Kind != domain.SourceKindPlaylist {
		return string(resumed)
	}
	return fmt.Sprintf(`{"kind":"playlist","playlist_id":%q,"name":%q}`,
		clientPlaylistId(source.PlaylistId), source.Name)
}

func resumedSource(t *testing.T, h *QueueHandler) json.RawMessage {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/queue-state", nil)
	req = req.WithContext(auth.ContextWithUserID(req.Context(), shared.NewUserId(uuid.New())))
	rec := httptest.NewRecorder()

	h.Routes().ServeHTTP(rec, req)

	var body map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode resume body %q: %v", rec.Body.String(), err)
	}
	return body["source"]
}

func TestQueueState_ResumedPlaylistWithoutAClientIdKeepsSaving(t *testing.T) {
	for name, storedSourceId := range map[string]string{
		"legacy id-less token":      "playlist::",
		"id outside client shape":   "playlist:a%3Ab:x",
		"id-less token with a name": "playlist::Road+trip",
	} {
		t.Run(name, func(t *testing.T) {
			h := newHandler(&recordingRepo{saved: &domain.QueueState{
				TrackIds: []string{"t1"}, NaturalOrder: []string{"t1"}, SourceId: storedSourceId,
			}})
			rec := httptest.NewRecorder()

			body := `{"track_ids":["t1"],"repeat_mode":"off","natural_order":["t1"],"source":` +
				resavedSource(t, resumedSource(t, h)) + `}`
			h.Routes().ServeHTTP(rec, savePut(body))

			if rec.Code != http.StatusNoContent {
				t.Fatalf("resaving a resumed queue must succeed, got status %d body %q for %s",
					rec.Code, rec.Body.String(), body)
			}
			if got := string(resumedSource(t, h)); got != "null" {
				t.Errorf("the resave stored a source that resumes into another rejected save: %s", got)
			}
		})
	}
}

func TestHandleGet_IdlessPlaylistRowResumesWithNoSource(t *testing.T) {
	h := newHandler(&recordingRepo{saved: &domain.QueueState{SourceId: "playlist::"}})

	got := string(resumedSource(t, h))

	if got != "null" {
		t.Errorf("a stored token naming no playlist must resume as no source, got %s", got)
	}
}

func TestHandleSave_IdlessPlaylistSourceIsStoredAsNoSource(t *testing.T) {
	repo := &recordingRepo{}
	h := newHandler(repo)
	rec := httptest.NewRecorder()

	h.Routes().ServeHTTP(rec, savePut(`{"track_ids":[],"repeat_mode":"off","source":{"kind":"playlist","playlist_id":"","name":"Road trip"}}`))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("a playlist source naming no playlist must not fail the save, got status %d body %q", rec.Code, rec.Body.String())
	}
	if repo.saved == nil || repo.saved.SourceId != "" {
		t.Errorf("a playlist source naming no playlist must be stored as no source, got %+v", repo.saved)
	}
}

func TestHandleSave_IdlessPlaylistLegacySourceIdIsStoredAsNoSource(t *testing.T) {
	repo := &recordingRepo{}
	h := newHandler(repo)
	rec := httptest.NewRecorder()

	h.Routes().ServeHTTP(rec, savePut(`{"track_ids":[],"repeat_mode":"off","source_id":"playlist::"}`))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("a legacy source_id naming no playlist must not fail the save, got status %d body %q", rec.Code, rec.Body.String())
	}
	if repo.saved == nil || repo.saved.SourceId != "" {
		t.Errorf("a legacy source_id naming no playlist must be stored as no source, got %+v", repo.saved)
	}
}

type failingNowPlaying struct{}

func (failingNowPlaying) Lookup(_ context.Context, _ shared.UserId, _ string) (*ports.NowPlayingTrack, error) {
	return nil, errors.New("lookup now-playing track: context deadline exceeded")
}

func getResume(t *testing.T, nowPlaying ports.NowPlayingReader, opts ...service.QueueServiceOption) (int, map[string]json.RawMessage) {
	t.Helper()
	repo := &recordingRepo{saved: &domain.QueueState{
		TrackIds:     []string{"t1", "t2"},
		CurrentIdx:   1,
		NaturalOrder: []string{"t1", "t2"},
	}}
	h := NewQueueHandler(service.NewQueueService(repo, nowPlaying, opts...))
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

type panickingNowPlaying struct{ t *testing.T }

func (p panickingNowPlaying) Lookup(_ context.Context, _ shared.UserId, _ string) (*ports.NowPlayingTrack, error) {
	p.t.Error("nowPlaying.Lookup called with PLAYBACK_NOW_PLAYING_ENRICHMENT_ENABLED=false")
	return nil, errors.New("lookup must not run while disabled")
}

func TestHandleGet_DisabledNowPlayingEnrichmentSkipsLookupAndStillResumes(t *testing.T) {
	code, body := getResume(t, panickingNowPlaying{t: t}, service.WithNowPlayingEnrichment(false))

	if code != http.StatusOK {
		t.Fatalf("resume must still succeed with 200 when enrichment is disabled, got %d", code)
	}
	if got := string(body["current_index"]); got != "1" {
		t.Errorf("resume must still return the queue state, current_index=%q", got)
	}
	if got := string(body["track_ids"]); got != `["t1","t2"]` {
		t.Errorf("resume must still return the queue track ids, got %s", got)
	}
	if v, ok := body["current_track"]; ok {
		t.Errorf("disabled enrichment must omit current_track, got %s", v)
	}
	if v, ok := body["current_track_unavailable"]; ok {
		t.Errorf("disabled enrichment is not a failure; current_track_unavailable must be omitted, got %s", v)
	}
}

func positionPut(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPut, "/queue-state/position", strings.NewReader(body))
	return req.WithContext(auth.ContextWithUserID(req.Context(), shared.NewUserId(uuid.New())))
}

func TestHandleSavePosition_WritesPositionWithoutTheQueue(t *testing.T) {
	repo := &recordingRepo{}
	rec := httptest.NewRecorder()

	newHandler(repo).Routes().ServeHTTP(rec, positionPut(`{"current_index":7,"current_track_id":"t8","position_ms":93000}`))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("position save must be 204, got %d body %q", rec.Code, rec.Body.String())
	}
	if repo.upserts != 0 {
		t.Fatalf("a position-only save ran the full-state Upsert %d time(s)", repo.upserts)
	}
	if p := repo.position; p == nil || p.CurrentIdx != 7 || p.CurrentTrackId != "t8" || p.PositionMs != 93000 {
		t.Fatalf("repository got position %+v, want idx 7, track t8, 93000ms", repo.position)
	}
}

func TestHandleSavePosition_InvalidBodyIsBadRequest(t *testing.T) {
	for name, body := range map[string]string{
		"missing track id":  `{"current_index":0,"position_ms":1}`,
		"negative position": `{"current_index":0,"current_track_id":"t1","position_ms":-5}`,
		"malformed json":    `{"current_index":`,
	} {
		t.Run(name, func(t *testing.T) {
			repo := &recordingRepo{}
			rec := httptest.NewRecorder()

			newHandler(repo).Routes().ServeHTTP(rec, positionPut(body))

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("got status %d body %q, want 400", rec.Code, rec.Body.String())
			}
			if repo.position != nil {
				t.Fatalf("an invalid position reached the repository: %+v", repo.position)
			}
		})
	}
}

func TestHandleSavePosition_UnappliedSaveIsConflictWithItsCode(t *testing.T) {
	for code, err := range map[string]error{
		"playback.stale_queue_write":       fmt.Errorf("update: %w", domain.ErrStaleQueueWrite),
		"playback.queue_position_mismatch": fmt.Errorf("update: %w", domain.ErrQueuePositionMismatch),
	} {
		t.Run(code, func(t *testing.T) {
			rec := httptest.NewRecorder()

			newHandler(&recordingRepo{positionErr: err}).Routes().ServeHTTP(rec, positionPut(`{"current_index":0,"current_track_id":"t1","position_ms":1}`))

			if rec.Code != http.StatusConflict {
				t.Fatalf("got status %d body %q, want 409", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), `"code":"`+code+`"`) {
				t.Errorf("expected code %q in body, got %q", code, rec.Body.String())
			}
		})
	}
}

func TestHandleSavePosition_SharesTheQueueStateRateLimit(t *testing.T) {
	h := NewQueueHandler(service.NewQueueService(&recordingRepo{}, nilNowPlaying{}),
		WithQueueStateRateLimit(QueueStateRateLimit{Every: time.Hour, Burst: 1}))
	user := shared.NewUserId(uuid.New())
	as := func(req *http.Request) *http.Request {
		return req.WithContext(auth.ContextWithUserID(req.Context(), user))
	}

	first := httptest.NewRecorder()
	h.Routes().ServeHTTP(first, as(savePut(`{"track_ids":[],"repeat_mode":"off","source_id":"library"}`)))
	second := httptest.NewRecorder()
	h.Routes().ServeHTTP(second, as(positionPut(`{"current_index":0,"current_track_id":"t1","position_ms":1}`)))

	if first.Code != http.StatusNoContent || second.Code != http.StatusTooManyRequests {
		t.Fatalf("full save then position save = %d, %d; want 204, 429 from one shared bucket", first.Code, second.Code)
	}
}

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
