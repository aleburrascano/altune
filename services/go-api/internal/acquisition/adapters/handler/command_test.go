package handler

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/acquisition/service"
	"altune/go-api/internal/auth"
	"altune/go-api/internal/shared"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	catdomain "altune/go-api/internal/catalog/domain"

	"github.com/go-chi/chi/v5"
)

// A cooldown refusal that carries no Retry-After leaves the client guessing the
// window, so both kinds assert the header alongside the 429 and its code.

func TestHandleRetryAcquisition_CooldownSendsRetryAfter(t *testing.T) {
	repo := newRetryFakeTrackRepo()
	track := newTrackOrFatal(t, retryTestUserId)
	_ = track.MarkFailed("download error")
	repo.seed(track)
	router := buildRetryRouter(repo, &retryFakeScheduler{})
	path := "/tracks/" + track.ID.UUID().String() + "/retry"

	retryAssertStatus(t, retryServe(t, router, http.MethodPost, path), http.StatusAccepted)

	assertCooldownRefusal(t, retryServe(t, router, http.MethodPost, path), service.RetryCooldown)
}

func TestHandleReacquire_CooldownSendsRetryAfter(t *testing.T) {
	repo := newRetryFakeTrackRepo()
	track := newTrackOrFatal(t, reacquireTestUserId)
	_ = track.MarkReady("audio/ready.opus")
	repo.seed(track)
	router := buildReacquireRouter(repo, &reacquireFakeScheduler{})
	path := "/tracks/" + track.ID.UUID().String() + "/reacquire"

	retryAssertStatus(t, retryServe(t, router, http.MethodPost, path), http.StatusAccepted)

	assertCooldownRefusal(t, retryServe(t, router, http.MethodPost, path), service.ReacquireCooldown)
}

func assertCooldownRefusal(t *testing.T, rec *httptest.ResponseRecorder, window time.Duration) {
	t.Helper()
	assertCooldownActive(t, rec)
	secs := retryAfterSeconds(t, rec.Header().Get("Retry-After"))
	if secs < 1 || secs > int(window.Seconds()) {
		t.Errorf("Retry-After = %d, want between 1 and %d", secs, int(window.Seconds()))
	}
}

func assertCooldownActive(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 within the cooldown (body: %s)", rec.Code, rec.Body)
	}
	var env struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode body: %v (raw: %s)", err, rec.Body)
	}
	if env.Code != "acquisition.cooldown_active" {
		t.Errorf("code = %q, want acquisition.cooldown_active", env.Code)
	}
}

func retryAfterSeconds(t *testing.T, header string) int {
	t.Helper()
	secs, err := strconv.Atoi(header)
	if err != nil {
		t.Fatalf("Retry-After = %q, want whole seconds: %v", header, err)
	}
	return secs
}

func newTrackOrFatal(t *testing.T, userId shared.UserId) *catdomain.Track {
	t.Helper()
	track, err := catdomain.NewTrack(userId, "Song", "Artist", "Album")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	return track
}

// A shed job must not answer 202: the caller sees a 503 with the queue-full
// code, and because nothing was queued the cooldown is not burned, so an
// immediate retry once the queue drains is accepted rather than 429'd.

func TestHandleRetryAcquisition_QueueFullSurfaces503AndKeepsCooldown(t *testing.T) {
	repo := newRetryFakeTrackRepo()
	scheduler := &retryFakeScheduler{err: service.ErrAcquisitionQueueFull}
	track := newTrackOrFatal(t, retryTestUserId)
	_ = track.MarkFailed("download error")
	repo.seed(track)
	router := buildRetryRouter(repo, scheduler)
	path := "/tracks/" + track.ID.UUID().String() + "/retry"

	rec := retryServe(t, router, http.MethodPost, path)
	assertQueueFull(t, rec.Code, rec.Body.Bytes())

	scheduler.err = nil
	retryAssertStatus(t, retryServe(t, router, http.MethodPost, path), http.StatusAccepted)
	if len(scheduler.scheduled) != 1 {
		t.Errorf("scheduled = %d, want 1 (only the post-drain retry queued)", len(scheduler.scheduled))
	}
}

func TestHandleReacquire_QueueFullSurfaces503AndKeepsCooldown(t *testing.T) {
	repo := newRetryFakeTrackRepo()
	scheduler := &reacquireFakeScheduler{err: service.ErrAcquisitionQueueFull}
	track := newTrackOrFatal(t, reacquireTestUserId)
	_ = track.MarkReady("audio/ready.opus")
	repo.seed(track)
	router := buildReacquireRouter(repo, scheduler)
	path := "/tracks/" + track.ID.UUID().String() + "/reacquire"

	rec := retryServe(t, router, http.MethodPost, path)
	assertQueueFull(t, rec.Code, rec.Body.Bytes())

	scheduler.err = nil
	retryAssertStatus(t, retryServe(t, router, http.MethodPost, path), http.StatusAccepted)
	if len(scheduler.replaced) != 1 {
		t.Errorf("replaced = %d, want 1 (only the post-drain reacquire queued)", len(scheduler.replaced))
	}
}

func assertQueueFull(t *testing.T, status int, body []byte) {
	t.Helper()
	if status != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 on a shed job (body: %s)", status, body)
	}
	var env struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode body: %v (raw: %s)", err, body)
	}
	if env.Code != "acquisition.queue_full" {
		t.Errorf("code = %q, want acquisition.queue_full", env.Code)
	}
}

// admitAlways stands in for the cooldown admissions: it admits every command,
// including the pending track both real admissions refuse, so a 202 here can
// only come from the handler holding this stub.
type admitAlways struct{}

func (admitAlways) Admit(_ context.Context, _ *catdomain.Track, schedule func() error) error {
	return schedule()
}

func TestHandleRetryAcquisition_UsesTheInjectedAdmission(t *testing.T) {
	repo := newRetryFakeTrackRepo()
	track := newTrackOrFatal(t, retryTestUserId)
	repo.seed(track)
	scheduler := &retryFakeScheduler{}
	h := NewRetryHandler(repo, scheduler, admitAlways{})
	router := chi.NewRouter()
	router.Use(auth.Middleware(retryVerifyAsTestUser))
	router.Post("/tracks/{trackId}/retry", h.HandleRetryAcquisition)

	rec := retryServe(t, router, http.MethodPost, "/tracks/"+track.ID.UUID().String()+"/retry")

	retryAssertStatus(t, rec, http.StatusAccepted)
	if len(scheduler.scheduled) != 1 {
		t.Errorf("scheduled = %d tracks, want 1", len(scheduler.scheduled))
	}
}

func TestHandleReacquire_UsesTheInjectedAdmission(t *testing.T) {
	repo := newRetryFakeTrackRepo()
	track := newTrackOrFatal(t, reacquireTestUserId)
	repo.seed(track)
	scheduler := &reacquireFakeScheduler{}
	h := NewReacquireHandler(repo, scheduler, admitAlways{})
	router := chi.NewRouter()
	router.Use(auth.Middleware(reacquireVerifyAsTestUser))
	router.Post("/tracks/{trackId}/reacquire", h.HandleReacquire)

	rec := retryServe(t, router, http.MethodPost, "/tracks/"+track.ID.UUID().String()+"/reacquire")

	retryAssertStatus(t, rec, http.StatusAccepted)
	if len(scheduler.replaced) != 1 {
		t.Errorf("replaced = %d tracks, want 1", len(scheduler.replaced))
	}
}

// memCooldownStore is an in-memory ports.CooldownStore for handler tests.
type memCooldownStore struct {
	mu     sync.Mutex
	lastAt map[string]time.Time
}

func newMemCooldownStore() *memCooldownStore {
	return &memCooldownStore{lastAt: make(map[string]time.Time)}
}

func (s *memCooldownStore) Reserve(_ context.Context, trackID catdomain.TrackId, kind ports.CooldownKind, cooldown time.Duration) (time.Time, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now, key := time.Now(), string(kind)+"/"+trackID.String()
	if last, ok := s.lastAt[key]; ok && now.Sub(last) < cooldown {
		return time.Time{}, false, nil
	}
	s.lastAt[key] = now
	return now, true, nil
}

func (s *memCooldownStore) Release(_ context.Context, trackID catdomain.TrackId, kind ports.CooldownKind, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := string(kind) + "/" + trackID.String()
	if last, ok := s.lastAt[key]; ok && last.Equal(at) {
		delete(s.lastAt, key)
	}
	return nil
}
