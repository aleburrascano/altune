package handler

import (
	authservice "altune/go-api/internal/auth/service"
	"altune/go-api/internal/playback/domain"
	"altune/go-api/internal/playback/ports"
	"altune/go-api/internal/playback/service/queue"
	"altune/go-api/internal/shared"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
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
	return NewQueueHandler(queue.New(repo, nilNowPlaying{}))
}

func savePut(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPut, "/queue-state", strings.NewReader(body))
	ctx := authservice.ContextWithUserID(req.Context(), shared.NewUserId(uuid.New()))
	return req.WithContext(ctx)
}

func deleteReq() *http.Request {
	req := httptest.NewRequest(http.MethodDelete, "/queue-state", nil)
	ctx := authservice.ContextWithUserID(req.Context(), shared.NewUserId(uuid.New()))
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
	req = req.WithContext(authservice.ContextWithUserID(req.Context(), shared.NewUserId(uuid.New())))
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
				TrackIds: []string{"00000000-0000-4000-8000-000000000001"}, NaturalOrder: []string{"00000000-0000-4000-8000-000000000001"}, SourceId: storedSourceId,
			}})
			rec := httptest.NewRecorder()

			body := `{"track_ids":["00000000-0000-4000-8000-000000000001"],"repeat_mode":"off","natural_order":["00000000-0000-4000-8000-000000000001"],"source":` +
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

func getResume(t *testing.T, nowPlaying ports.NowPlayingReader, opts ...queue.Option) (int, map[string]json.RawMessage) {
	t.Helper()
	repo := &recordingRepo{saved: &domain.QueueState{
		TrackIds:     []string{"00000000-0000-4000-8000-000000000001", "00000000-0000-4000-8000-000000000002"},
		CurrentIdx:   1,
		NaturalOrder: []string{"00000000-0000-4000-8000-000000000001", "00000000-0000-4000-8000-000000000002"},
	}}
	h := NewQueueHandler(queue.New(repo, nowPlaying, opts...))
	req := httptest.NewRequest(http.MethodGet, "/queue-state", nil)
	req = req.WithContext(authservice.ContextWithUserID(req.Context(), shared.NewUserId(uuid.New())))
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
	code, body := getResume(t, panickingNowPlaying{t: t}, queue.WithNowPlayingEnrichment(false))

	if code != http.StatusOK {
		t.Fatalf("resume must still succeed with 200 when enrichment is disabled, got %d", code)
	}
	if got := string(body["current_index"]); got != "1" {
		t.Errorf("resume must still return the queue state, current_index=%q", got)
	}
	if got := string(body["track_ids"]); got != `["00000000-0000-4000-8000-000000000001","00000000-0000-4000-8000-000000000002"]` {
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
	return req.WithContext(authservice.ContextWithUserID(req.Context(), shared.NewUserId(uuid.New())))
}

func TestHandleSavePosition_WritesPositionWithoutTheQueue(t *testing.T) {
	repo := &recordingRepo{}
	rec := httptest.NewRecorder()

	newHandler(repo).Routes().ServeHTTP(rec, positionPut(`{"current_index":7,"current_track_id":"00000000-0000-4000-8000-000000000008","position_ms":93000}`))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("position save must be 204, got %d body %q", rec.Code, rec.Body.String())
	}
	if repo.upserts != 0 {
		t.Fatalf("a position-only save ran the full-state Upsert %d time(s)", repo.upserts)
	}
	if p := repo.position; p == nil || p.CurrentIdx != 7 || p.CurrentTrackId != "00000000-0000-4000-8000-000000000008" || p.PositionMs != 93000 {
		t.Fatalf("repository got position %+v, want idx 7, the eighth track, 93000ms", repo.position)
	}
}

func TestHandleSavePosition_InvalidBodyIsBadRequest(t *testing.T) {
	for name, body := range map[string]string{
		"missing track id":  `{"current_index":0,"position_ms":1}`,
		"negative position": `{"current_index":0,"current_track_id":"00000000-0000-4000-8000-000000000001","position_ms":-5}`,
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

			newHandler(&recordingRepo{positionErr: err}).Routes().ServeHTTP(rec, positionPut(`{"current_index":0,"current_track_id":"00000000-0000-4000-8000-000000000001","position_ms":1}`))

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
	h := NewQueueHandler(queue.New(&recordingRepo{}, nilNowPlaying{}),
		WithQueueStateRateLimit(QueueStateRateLimit{Every: time.Hour, Burst: 1}))
	user := shared.NewUserId(uuid.New())
	as := func(req *http.Request) *http.Request {
		return req.WithContext(authservice.ContextWithUserID(req.Context(), user))
	}

	first := httptest.NewRecorder()
	h.Routes().ServeHTTP(first, as(savePut(`{"track_ids":[],"repeat_mode":"off","source_id":"library"}`)))
	second := httptest.NewRecorder()
	h.Routes().ServeHTTP(second, as(positionPut(`{"current_index":0,"current_track_id":"00000000-0000-4000-8000-000000000001","position_ms":1}`)))

	if first.Code != http.StatusNoContent || second.Code != http.StatusTooManyRequests {
		t.Fatalf("full save then position save = %d, %d; want 204, 429 from one shared bucket", first.Code, second.Code)
	}
}

func TestHandleGet_ResponseIsUncachedAndNotSniffable(t *testing.T) {
	h := NewQueueHandler(queue.New(&recordingRepo{saved: &domain.QueueState{
		TrackIds:     []string{"00000000-0000-4000-8000-000000000001"},
		NaturalOrder: []string{"00000000-0000-4000-8000-000000000001"},
	}}, nilNowPlaying{}))
	req := httptest.NewRequest(http.MethodGet, "/queue-state", nil)
	req = req.WithContext(authservice.ContextWithUserID(req.Context(), shared.NewUserId(uuid.New())))
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
	return fmt.Errorf("upsert: %w", domain.ErrQueueStateUnavailable)
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

func TestHandleSave_UnavailableWireFormat(t *testing.T) {
	rec := httptest.NewRecorder()

	newHandler(&unavailableRepo{}).Routes().ServeHTTP(rec, savePut(validSaveBody))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503, body %q", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Retry-After"); got != "1" {
		t.Errorf("Retry-After = %q, want 1", got)
	}
	const want = `{"detail":"queue state temporarily unavailable","code":"playback.unavailable"}`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Errorf("body = %s, want %s", got, want)
	}
}

type lookAlikeRepo struct {
	recordingRepo
}

func (lookAlikeRepo) Upsert(_ context.Context, _ *domain.QueueState) error {
	return errors.New("queue state temporarily unavailable")
}

func TestHandleSave_LookAlikeUnavailableTextIsNot503(t *testing.T) {
	rec := httptest.NewRecorder()

	newHandler(&lookAlikeRepo{}).Routes().ServeHTTP(rec, savePut(validSaveBody))

	if rec.Code == http.StatusServiceUnavailable {
		t.Fatalf("a plain error with the same text must not map to 503, body %q", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "playback.unavailable") {
		t.Errorf("body = %q, must not carry the unavailable code", rec.Body.String())
	}
}

type cancelingPositionRepo struct {
	recordingRepo
	cancel context.CancelFunc
}

func (r *cancelingPositionRepo) UpdatePosition(ctx context.Context, _ *domain.QueuePosition) error {
	r.cancel()
	<-ctx.Done()
	return ctx.Err()
}

type levelRecorder struct {
	mu     sync.Mutex
	levels []slog.Level
}

func (l *levelRecorder) Enabled(context.Context, slog.Level) bool { return true }

func (l *levelRecorder) Handle(_ context.Context, rec slog.Record) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.levels = append(l.levels, rec.Level)
	return nil
}

func (l *levelRecorder) WithAttrs([]slog.Attr) slog.Handler { return l }

func (l *levelRecorder) WithGroup(string) slog.Handler { return l }

func (l *levelRecorder) errorCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, lv := range l.levels {
		if lv >= slog.LevelError {
			n++
		}
	}
	return n
}

func recordLogLevels(t *testing.T) *levelRecorder {
	t.Helper()
	rec := &levelRecorder{}
	prev := slog.Default()
	slog.SetDefault(slog.New(rec))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return rec
}

func TestHandleSavePosition_ClientCancelIsNotAnErrorOr5xx(t *testing.T) {
	logs := recordLogLevels(t)
	req := positionPut(`{"current_index":0,"current_track_id":"00000000-0000-4000-8000-000000000001","position_ms":1}`)
	ctx, cancel := context.WithCancel(req.Context())
	defer cancel()
	rec := httptest.NewRecorder()

	newHandler(&cancelingPositionRepo{cancel: cancel}).Routes().ServeHTTP(rec, req.WithContext(ctx))

	if rec.Code >= http.StatusInternalServerError {
		t.Fatalf("a client disconnect returned %d, want no 5xx (body %q)", rec.Code, rec.Body.String())
	}
	if n := logs.errorCount(); n != 0 {
		t.Fatalf("a client disconnect logged %d ERROR record(s), want 0", n)
	}
}

func TestHandleSavePosition_CanceledErrorWithLiveContextStays500(t *testing.T) {
	rec := httptest.NewRecorder()

	newHandler(&recordingRepo{positionErr: fmt.Errorf("update: %w", context.Canceled)}).Routes().ServeHTTP(rec, positionPut(`{"current_index":0,"current_track_id":"00000000-0000-4000-8000-000000000001","position_ms":1}`))

	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), `"code":"internal"`) {
		t.Fatalf("a cancel the client did not cause must stay 500 internal, got %d body %q", rec.Code, rec.Body.String())
	}
}

func TestHandleSavePosition_LiveContextErrorsKeepTheirStatus(t *testing.T) {
	for name, tc := range map[string]struct {
		err    error
		status int
		code   string
	}{
		"internal":    {errors.New("boom"), http.StatusInternalServerError, "internal"},
		"unavailable": {fmt.Errorf("update: %w", domain.ErrQueueStateUnavailable), http.StatusServiceUnavailable, "playback.unavailable"},
		"conflict":    {fmt.Errorf("update: %w", domain.ErrStaleQueueWrite), http.StatusConflict, "playback.stale_queue_write"},
	} {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()

			newHandler(&recordingRepo{positionErr: tc.err}).Routes().ServeHTTP(rec, positionPut(`{"current_index":0,"current_track_id":"00000000-0000-4000-8000-000000000001","position_ms":1}`))

			if rec.Code != tc.status || !strings.Contains(rec.Body.String(), `"code":"`+tc.code+`"`) {
				t.Fatalf("got %d body %q, want %d with code %q", rec.Code, rec.Body.String(), tc.status, tc.code)
			}
		})
	}
}

type exactPositionRepo struct {
	recordingRepo
}

func (r *exactPositionRepo) UpdatePosition(_ context.Context, position *domain.QueuePosition) error {
	stored := r.saved
	if stored == nil || position.CurrentIdx >= len(stored.TrackIds) || stored.TrackIds[position.CurrentIdx] != position.CurrentTrackId {
		return domain.ErrQueuePositionMismatch
	}
	r.position = position
	return nil
}

func asUser(req *http.Request, user shared.UserId) *http.Request {
	return req.WithContext(authservice.ContextWithUserID(req.Context(), user))
}

func TestHandleSavePosition_CaseDifferingTrackIdIsNotAConflict(t *testing.T) {
	h := newHandler(&exactPositionRepo{})
	user := shared.NewUserId(uuid.New())
	saved := httptest.NewRecorder()
	h.Routes().ServeHTTP(saved, asUser(savePut(`{"track_ids":["3F2504E0-4F89-11D3-9A0C-0305E82C3301"],"current_index":0,"repeat_mode":"off"}`), user))
	if saved.Code != http.StatusNoContent {
		t.Fatalf("save got status %d body %q, want 204", saved.Code, saved.Body.String())
	}

	rec := httptest.NewRecorder()
	h.Routes().ServeHTTP(rec, asUser(positionPut(`{"current_index":0,"current_track_id":"3f2504e0-4f89-11d3-9a0c-0305e82c3301","position_ms":1}`), user))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("a position write for the same track in another case got status %d body %q, want 204", rec.Code, rec.Body.String())
	}
}

func TestHandleSave_GarbageTrackIdsAreRejectedNotStored(t *testing.T) {
	for name, body := range map[string]string{
		"unparseable id":           `{"track_ids":["not-a-uuid"],"repeat_mode":"off"}`,
		"empty elements":           `{"track_ids":["a","",""],"repeat_mode":"off","natural_order":["x"]}`,
		"natural order of another": `{"track_ids":["3f2504e0-4f89-11d3-9a0c-0305e82c3301"],"repeat_mode":"off","natural_order":["6ba7b810-9dad-11d1-80b4-00c04fd430c8"]}`,
	} {
		t.Run(name, func(t *testing.T) {
			repo := &recordingRepo{}
			rec := httptest.NewRecorder()

			newHandler(repo).Routes().ServeHTTP(rec, savePut(body))

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("got status %d body %q, want 400", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), `"code":"playback.`) {
				t.Errorf("want a playback validation code, got %q", rec.Body.String())
			}
			if repo.saved != nil {
				t.Fatalf("a rejected queue was stored: %+v", repo.saved)
			}
		})
	}
}

func TestHandleSavePosition_GarbageTrackIdIsBadRequest(t *testing.T) {
	repo := &recordingRepo{}
	rec := httptest.NewRecorder()

	newHandler(repo).Routes().ServeHTTP(rec, positionPut(`{"current_index":0,"current_track_id":"not-a-uuid","position_ms":1}`))

	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"code":"playback.malformed_track_id"`) {
		t.Fatalf("got status %d body %q, want 400 playback.malformed_track_id", rec.Code, rec.Body.String())
	}
	if repo.position != nil {
		t.Fatalf("a garbage track id reached the repository: %+v", repo.position)
	}
}

func TestHandleSave_BadSourceWinsOverBadRepeatMode(t *testing.T) {
	rec := httptest.NewRecorder()

	newHandler(&recordingRepo{}).Routes().ServeHTTP(rec, savePut(`{"track_ids":[],"repeat_mode":"sideways","source_id":"mixtape:7"}`))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got status %d body %q, want 400", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"playback.unrecognized_source_id"`) {
		t.Errorf("the source error must win over the repeat-mode error, got %q", rec.Body.String())
	}
}

func TestHandleForget_AuditRecordNamesTheSelfServiceTrigger(t *testing.T) {
	var buf strings.Builder
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	rec := httptest.NewRecorder()

	newHandler(&recordingRepo{}).Routes().ServeHTTP(rec, deleteReq())

	if rec.Code != http.StatusNoContent {
		t.Fatalf("got status %d body %q, want 204", rec.Code, rec.Body.String())
	}
	var audit map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var entry map[string]any
		if json.Unmarshal([]byte(line), &entry) == nil && entry["msg"] == "playback.queue_state_forgotten" {
			audit = entry
		}
	}
	if audit == nil {
		t.Fatalf("no playback.queue_state_forgotten record; logs=%s", buf.String())
	}
	if audit["trigger"] != "self" {
		t.Errorf("trigger = %v, want self; record=%v", audit["trigger"], audit)
	}
}
