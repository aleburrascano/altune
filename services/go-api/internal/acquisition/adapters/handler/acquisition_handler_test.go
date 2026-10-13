package handler

import (
	"altune/go-api/internal/acquisition/adapters/catalogbridge"
	acqdomain "altune/go-api/internal/acquisition/domain"
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/acquisition/service"
	"altune/go-api/internal/auth/adapters/middleware"
	authdomain "altune/go-api/internal/auth/domain"
	authports "altune/go-api/internal/auth/ports"
	catdomain "altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

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

func TestHandleRetryAcquisition_CooldownRetryAfterIsTimeLeft(t *testing.T) {
	repo := newRetryFakeTrackRepo()
	track := newTrackOrFatal(t, retryTestUserId)
	_ = track.MarkFailed("download error")
	repo.seed(track)
	store := newMemCooldownStore()
	clock := time.Now()
	store.now = func() time.Time { return clock }
	h := NewRetryHandler(catalogbridge.NewTrackLedger(repo), &retryFakeScheduler{}, service.NewRetryAdmission(store))
	router := chi.NewRouter()
	router.Use(middleware.Middleware(retryVerifyAsTestUser))
	router.Post("/tracks/{trackId}/retry", h.HandleRetryAcquisition)
	path := "/tracks/" + track.ID.UUID().String() + "/retry"

	retryAssertStatus(t, retryServe(t, router, http.MethodPost, path), http.StatusAccepted)
	clock = clock.Add(45*time.Second + 500*time.Millisecond)

	rec := retryServe(t, router, http.MethodPost, path)
	assertCooldownActive(t, rec)
	if got := retryAfterSeconds(t, rec.Header().Get("Retry-After")); got != 15 {
		t.Errorf("Retry-After = %d, want 15 (time left, rounded up)", got)
	}
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

func TestHandleRetryAcquisition_ShuttingDownSurfaces503AndKeepsCooldown(t *testing.T) {
	repo := newRetryFakeTrackRepo()
	scheduler := &retryFakeScheduler{err: acqdomain.ErrSchedulerShutdown}
	track := newTrackOrFatal(t, retryTestUserId)
	_ = track.MarkFailed("download error")
	repo.seed(track)
	router := buildRetryRouter(repo, scheduler)
	path := "/tracks/" + track.ID.UUID().String() + "/retry"

	rec := retryServe(t, router, http.MethodPost, path)
	assertShuttingDown(t, rec.Code, rec.Body.Bytes())

	scheduler.err = nil
	retryAssertStatus(t, retryServe(t, router, http.MethodPost, path), http.StatusAccepted)
	if len(scheduler.scheduled) != 1 {
		t.Errorf("scheduled = %d, want 1 (only the post-drain retry queued)", len(scheduler.scheduled))
	}
}

func TestHandleReacquire_ShuttingDownSurfaces503AndKeepsCooldown(t *testing.T) {
	repo := newRetryFakeTrackRepo()
	scheduler := &reacquireFakeScheduler{err: acqdomain.ErrSchedulerShutdown}
	track := newTrackOrFatal(t, reacquireTestUserId)
	_ = track.MarkReady("audio/ready.opus")
	repo.seed(track)
	router := buildReacquireRouter(repo, scheduler)
	path := "/tracks/" + track.ID.UUID().String() + "/reacquire"

	rec := retryServe(t, router, http.MethodPost, path)
	assertShuttingDown(t, rec.Code, rec.Body.Bytes())

	scheduler.err = nil
	retryAssertStatus(t, retryServe(t, router, http.MethodPost, path), http.StatusAccepted)
	if len(scheduler.replaced) != 1 {
		t.Errorf("replaced = %d, want 1 (only the post-drain reacquire queued)", len(scheduler.replaced))
	}
}

func assertShuttingDown(t *testing.T, status int, body []byte) {
	t.Helper()
	if status != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 while shutting down (body: %s)", status, body)
	}
	var env struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode body: %v (raw: %s)", err, body)
	}
	if env.Code != "acquisition.shutting_down" {
		t.Errorf("code = %q, want acquisition.shutting_down", env.Code)
	}
}

type admitAlways struct{}

func (admitAlways) Admit(_ context.Context, _ *ports.TrackSnapshot, schedule func() error) error {
	return schedule()
}

func TestHandleRetryAcquisition_UsesTheInjectedAdmission(t *testing.T) {
	repo := newRetryFakeTrackRepo()
	track := newTrackOrFatal(t, retryTestUserId)
	repo.seed(track)
	scheduler := &retryFakeScheduler{}
	h := NewRetryHandler(catalogbridge.NewTrackLedger(repo), scheduler, admitAlways{})
	router := chi.NewRouter()
	router.Use(middleware.Middleware(retryVerifyAsTestUser))
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
	h := NewReacquireHandler(catalogbridge.NewTrackLedger(repo), scheduler, admitAlways{})
	router := chi.NewRouter()
	router.Use(middleware.Middleware(reacquireVerifyAsTestUser))
	router.Post("/tracks/{trackId}/reacquire", h.HandleReacquire)

	rec := retryServe(t, router, http.MethodPost, "/tracks/"+track.ID.UUID().String()+"/reacquire")

	retryAssertStatus(t, rec, http.StatusAccepted)
	if len(scheduler.replaced) != 1 {
		t.Errorf("replaced = %d tracks, want 1", len(scheduler.replaced))
	}
}

type memCooldownStore struct {
	mu     sync.Mutex
	now    func() time.Time
	lastAt map[string]time.Time
}

func newMemCooldownStore() *memCooldownStore {
	return &memCooldownStore{now: time.Now, lastAt: make(map[string]time.Time)}
}

func (s *memCooldownStore) Reserve(_ context.Context, trackID catdomain.TrackId, kind ports.CooldownKind, cooldown time.Duration) (time.Time, bool, time.Duration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now, key := s.now(), string(kind)+"/"+trackID.String()
	if last, ok := s.lastAt[key]; ok && now.Sub(last) < cooldown {
		return time.Time{}, false, cooldown - now.Sub(last), nil
	}
	s.lastAt[key] = now
	return now, true, 0, nil
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

var (
	reacquireTestUserUUID = uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	reacquireTestUserId   = shared.NewUserId(reacquireTestUserUUID)
)

var reacquireVerifyAsTestUser = authports.VerifierFunc(func(context.Context, string) (authdomain.VerifiedToken, error) {
	return authdomain.VerifiedToken{UserID: reacquireTestUserId, ExpiresAt: time.Now().Add(time.Hour)}, nil
})

type reacquireFakeScheduler struct {
	replaced []catdomain.TrackId
	err      error
}

func (s *reacquireFakeScheduler) ScheduleReplace(_ context.Context, _ shared.UserId, trackId catdomain.TrackId) error {
	if s.err != nil {
		return s.err
	}
	s.replaced = append(s.replaced, trackId)
	return nil
}

func makeReacquireTrack(userId shared.UserId, title, artist, album string) *catdomain.Track {
	t, err := catdomain.NewTrack(userId, title, artist, album)
	if err != nil {
		panic(err)
	}
	return t
}

func makeStreamableReacquireTrack(userId shared.UserId, title, artist, album, audioRef string) *catdomain.Track {
	t := makeReacquireTrack(userId, title, artist, album)
	_ = t.MarkReady(audioRef)
	return t
}

func buildReacquireRouter(trackRepo *retryFakeTrackRepo, scheduler *reacquireFakeScheduler) chi.Router {
	h := NewReacquireHandler(catalogbridge.NewTrackLedger(trackRepo), scheduler, service.NewReacquireAdmission(newMemCooldownStore()))
	r := chi.NewRouter()
	r.Use(middleware.Middleware(reacquireVerifyAsTestUser))
	r.Post("/tracks/{trackId}/reacquire", h.HandleReacquire)
	return r
}

func TestHandleReacquire(t *testing.T) {
	tests := []struct {
		name         string
		setup        func(*retryFakeTrackRepo) string
		wantStatus   int
		wantReplaced bool
	}{
		{
			name: "streamable track returns 202 and schedules replace",
			setup: func(repo *retryFakeTrackRepo) string {
				track := makeStreamableReacquireTrack(reacquireTestUserId, "Ready Song", "Artist", "Album", "audio/ready.opus")
				repo.seed(track)
				return track.ID.UUID().String()
			},
			wantStatus:   http.StatusAccepted,
			wantReplaced: true,
		},
		{
			name: "pending track (no audio) returns 409",
			setup: func(repo *retryFakeTrackRepo) string {
				track := makeReacquireTrack(reacquireTestUserId, "Pending Song", "Artist", "Album")
				repo.seed(track)
				return track.ID.UUID().String()
			},
			wantStatus: http.StatusConflict,
		},
		{
			name: "failed track (no audio) returns 409",
			setup: func(repo *retryFakeTrackRepo) string {
				track := makeReacquireTrack(reacquireTestUserId, "Failed Song", "Artist", "Album")
				_ = track.MarkFailed("download error")
				repo.seed(track)
				return track.ID.UUID().String()
			},
			wantStatus: http.StatusConflict,
		},
		{
			name: "not found returns 404",
			setup: func(repo *retryFakeTrackRepo) string {
				return uuid.New().String()
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "invalid track ID returns 400",
			setup: func(repo *retryFakeTrackRepo) string {
				return "not-a-uuid"
			},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newRetryFakeTrackRepo()
			scheduler := &reacquireFakeScheduler{}
			trackId := tt.setup(repo)
			router := buildReacquireRouter(repo, scheduler)

			rec := retryServe(t, router, http.MethodPost, "/tracks/"+trackId+"/reacquire")

			retryAssertStatus(t, rec, tt.wantStatus)

			if tt.wantReplaced && len(scheduler.replaced) == 0 {
				t.Error("expected scheduler to be called, but no track was scheduled for replace")
			}
			if !tt.wantReplaced && len(scheduler.replaced) > 0 {
				t.Errorf("expected no scheduling, but %d tracks were scheduled for replace", len(scheduler.replaced))
			}
		})
	}
}

func TestHandleReacquire_Cooldown(t *testing.T) {
	repo := newRetryFakeTrackRepo()
	scheduler := &reacquireFakeScheduler{}
	track := makeStreamableReacquireTrack(reacquireTestUserId, "Ready Song", "Artist", "Album", "audio/ready.opus")
	repo.seed(track)
	router := buildReacquireRouter(repo, scheduler)
	path := "/tracks/" + track.ID.UUID().String() + "/reacquire"

	first := retryServe(t, router, http.MethodPost, path)
	retryAssertStatus(t, first, http.StatusAccepted)

	second := retryServe(t, router, http.MethodPost, path)
	retryAssertStatus(t, second, http.StatusTooManyRequests)

	if len(scheduler.replaced) != 1 {
		t.Errorf("expected exactly one scheduled replace, got %d", len(scheduler.replaced))
	}
}

func TestHandleReacquire_CooldownUsesStandardErrorEnvelope(t *testing.T) {
	repo := newRetryFakeTrackRepo()
	scheduler := &reacquireFakeScheduler{}
	track := makeStreamableReacquireTrack(reacquireTestUserId, "Ready Song", "Artist", "Album", "audio/ready.opus")
	repo.seed(track)
	router := buildReacquireRouter(repo, scheduler)
	path := "/tracks/" + track.ID.UUID().String() + "/reacquire"

	retryAssertStatus(t, retryServe(t, router, http.MethodPost, path), http.StatusAccepted)

	rec := retryServe(t, router, http.MethodPost, path)
	retryAssertStatus(t, rec, http.StatusTooManyRequests)

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v (raw: %s)", err, rec.Body.String())
	}
	if _, legacy := body["error"]; legacy {
		t.Errorf("429 body uses legacy {\"error\":...} envelope, want standard {\"detail\",\"code\"}: %s", rec.Body.String())
	}
	if detail, _ := body["detail"].(string); detail == "" {
		t.Errorf("429 body missing \"detail\", want standard envelope: %s", rec.Body.String())
	}
	if code, _ := body["code"].(string); code == "" {
		t.Errorf("429 body missing \"code\", want standard envelope: %s", rec.Body.String())
	}
}

func TestHandleReacquire_NoAuth(t *testing.T) {
	repo := newRetryFakeTrackRepo()
	scheduler := &reacquireFakeScheduler{}
	router := buildReacquireRouter(repo, scheduler)

	req := httptest.NewRequest(http.MethodPost, "/tracks/"+uuid.New().String()+"/reacquire", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	retryAssertStatus(t, rec, http.StatusUnauthorized)
}

var (
	retryTestUserUUID = uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	retryTestUserId   = shared.NewUserId(retryTestUserUUID)
)

var retryVerifyAsTestUser = authports.VerifierFunc(func(context.Context, string) (authdomain.VerifiedToken, error) {
	return authdomain.VerifiedToken{UserID: retryTestUserId, ExpiresAt: time.Now().Add(time.Hour)}, nil
})

type retryFakeTrackRepo struct {
	tracks map[string]*catdomain.Track
	getErr error
}

func newRetryFakeTrackRepo() *retryFakeTrackRepo {
	return &retryFakeTrackRepo{tracks: make(map[string]*catdomain.Track)}
}

func (r *retryFakeTrackRepo) Add(_ context.Context, track *catdomain.Track) (bool, error) {
	r.tracks[track.ID.String()] = track
	return true, nil
}

func (r *retryFakeTrackRepo) GetByID(_ context.Context, id catdomain.TrackId, userId shared.UserId) (*catdomain.Track, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	for _, t := range r.tracks {
		if t.ID == id && t.UserId == userId {
			return t, nil
		}
	}
	return nil, nil
}

func (r *retryFakeTrackRepo) AudioRefInUse(_ context.Context, audioRef string, excludeTrackID catdomain.TrackId) (bool, error) {
	for _, t := range r.tracks {
		if t.ID != excludeTrackID && t.AudioRef != nil && *t.AudioRef == audioRef {
			return true, nil
		}
	}
	return false, nil
}

func (r *retryFakeTrackRepo) ListForUser(_ context.Context, _ shared.UserId, _, _ int) ([]*catdomain.Track, int, error) {
	return nil, 0, nil
}

func (r *retryFakeTrackRepo) Update(_ context.Context, track *catdomain.Track, _ int) error {
	r.tracks[track.ID.String()] = track
	return nil
}

func (r *retryFakeTrackRepo) Delete(_ context.Context, id catdomain.TrackId, _ shared.UserId) (bool, error) {
	key := id.String()
	if _, ok := r.tracks[key]; ok {
		delete(r.tracks, key)
		return true, nil
	}
	return false, nil
}

func (r *retryFakeTrackRepo) GetByDedupKey(_ context.Context, _ shared.UserId, _ string) (*catdomain.Track, error) {
	return nil, nil
}

func (r *retryFakeTrackRepo) seed(t *catdomain.Track) {
	r.tracks[t.ID.String()] = t
}

type retryFakeScheduler struct {
	scheduled []catdomain.TrackId
	err       error
}

func (s *retryFakeScheduler) Schedule(_ context.Context, _ shared.UserId, trackId catdomain.TrackId, _ string) error {
	if s.err != nil {
		return s.err
	}
	s.scheduled = append(s.scheduled, trackId)
	return nil
}

func retryServe(t *testing.T, router chi.Router, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", "Bearer fake-token")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func retryAssertStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Errorf("status = %d, want %d (body: %s)", rec.Code, want, rec.Body.String())
	}
}

var _ = io.NopCloser

func makeRetryTrack(userId shared.UserId, title, artist, album string) *catdomain.Track {
	t, err := catdomain.NewTrack(userId, title, artist, album)
	if err != nil {
		panic(err)
	}
	return t
}

func makeFailedTrack(userId shared.UserId, title, artist, album string) *catdomain.Track {
	t := makeRetryTrack(userId, title, artist, album)
	_ = t.MarkFailed("download error")
	return t
}

func makeReadyRetryTrack(userId shared.UserId, title, artist, album, audioRef string) *catdomain.Track {
	t := makeRetryTrack(userId, title, artist, album)
	_ = t.MarkReady(audioRef)
	return t
}

func buildRetryRouter(trackRepo *retryFakeTrackRepo, scheduler *retryFakeScheduler) chi.Router {
	h := NewRetryHandler(catalogbridge.NewTrackLedger(trackRepo), scheduler, service.NewRetryAdmission(newMemCooldownStore()))
	r := chi.NewRouter()
	r.Use(middleware.Middleware(retryVerifyAsTestUser))
	r.Post("/tracks/{trackId}/retry", h.HandleRetryAcquisition)
	return r
}

func TestHandleRetryAcquisition(t *testing.T) {
	tests := []struct {
		name          string
		setup         func(*retryFakeTrackRepo) string
		wantStatus    int
		wantScheduled bool
	}{
		{
			name: "failed track returns 202 and schedules retry",
			setup: func(repo *retryFakeTrackRepo) string {
				track := makeFailedTrack(retryTestUserId, "Failed Song", "Artist", "Album")
				repo.seed(track)
				return track.ID.UUID().String()
			},
			wantStatus:    http.StatusAccepted,
			wantScheduled: true,
		},
		{
			name: "not found returns 404",
			setup: func(repo *retryFakeTrackRepo) string {
				return uuid.New().String()
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "pending track (not failed) returns 409",
			setup: func(repo *retryFakeTrackRepo) string {
				track := makeRetryTrack(retryTestUserId, "Pending Song", "Artist", "Album")
				repo.seed(track)
				return track.ID.UUID().String()
			},
			wantStatus: http.StatusConflict,
		},
		{
			name: "ready track (not failed) returns 409",
			setup: func(repo *retryFakeTrackRepo) string {
				track := makeReadyRetryTrack(retryTestUserId, "Ready Song", "Artist", "Album", "audio/ready.opus")
				repo.seed(track)
				return track.ID.UUID().String()
			},
			wantStatus: http.StatusConflict,
		},
		{
			name: "invalid track ID returns 400",
			setup: func(repo *retryFakeTrackRepo) string {
				return "not-a-uuid"
			},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newRetryFakeTrackRepo()
			scheduler := &retryFakeScheduler{}
			trackId := tt.setup(repo)
			router := buildRetryRouter(repo, scheduler)

			rec := retryServe(t, router, http.MethodPost, "/tracks/"+trackId+"/retry")

			retryAssertStatus(t, rec, tt.wantStatus)

			if tt.wantScheduled && len(scheduler.scheduled) == 0 {
				t.Error("expected scheduler to be called, but no track was scheduled")
			}
			if !tt.wantScheduled && len(scheduler.scheduled) > 0 {
				t.Errorf("expected no scheduling, but %d tracks were scheduled", len(scheduler.scheduled))
			}
		})
	}
}

func TestHandleRetryAcquisition_NoAuth(t *testing.T) {
	repo := newRetryFakeTrackRepo()
	scheduler := &retryFakeScheduler{}
	router := buildRetryRouter(repo, scheduler)

	req := httptest.NewRequest(http.MethodPost, "/tracks/"+uuid.New().String()+"/retry", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	retryAssertStatus(t, rec, http.StatusUnauthorized)
}
