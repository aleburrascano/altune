package handler_test

import (
	"altune/go-api/internal/download"
	"altune/go-api/internal/download/internal/adapters/acquisitiontest"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/requestuser"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func TestHandleRetryAcquisition_CooldownSendsRetryAfter(t *testing.T) {
	repo := acquisitiontest.NewLedger()
	track := failedTrack(retryTestUserId)
	repo.Seed(track)
	router := buildRetryRouter(repo, &retryFakeScheduler{})
	path := "/tracks/" + track.ID.UUID().String() + "/retry"

	retryAssertStatus(t, retryServe(t, router, http.MethodPost, path), http.StatusAccepted)

	assertCooldownRefusal(t, retryServe(t, router, http.MethodPost, path), cooldownWindow)
}

func TestHandleReacquire_CooldownSendsRetryAfter(t *testing.T) {
	repo := acquisitiontest.NewLedger()
	track := readyTrack(reacquireTestUserId)
	repo.Seed(track)
	router := buildReacquireRouter(repo, &reacquireFakeScheduler{})
	path := "/tracks/" + track.ID.UUID().String() + "/reacquire"

	retryAssertStatus(t, retryServe(t, router, http.MethodPost, path), http.StatusAccepted)

	assertCooldownRefusal(t, retryServe(t, router, http.MethodPost, path), cooldownWindow)
}

func TestHandleRetryAcquisition_CooldownRetryAfterIsTimeLeft(t *testing.T) {
	repo := acquisitiontest.NewLedger()
	track := failedTrack(retryTestUserId)
	repo.Seed(track)
	store := newMemCooldownStore()
	clock := time.Now()
	store.now = func() time.Time { return clock }
	h := download.NewRetryHandler(repo, &retryFakeScheduler{}, download.NewRetryAdmission(store))
	router := chi.NewRouter()
	router.Use(signedInAs(retryTestUserId))
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

const cooldownWindow = time.Minute

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

func TestHandleRetryAcquisition_ShuttingDownSurfaces503AndKeepsCooldown(t *testing.T) {
	repo := acquisitiontest.NewLedger()
	scheduler := &retryFakeScheduler{err: download.ErrSchedulerShutdown}
	track := failedTrack(retryTestUserId)
	repo.Seed(track)
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
	repo := acquisitiontest.NewLedger()
	scheduler := &reacquireFakeScheduler{err: download.ErrSchedulerShutdown}
	track := readyTrack(reacquireTestUserId)
	repo.Seed(track)
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

func (admitAlways) Admit(_ context.Context, _ *download.TrackSnapshot, schedule func() error) error {
	return schedule()
}

func TestHandleRetryAcquisition_UsesTheInjectedAdmission(t *testing.T) {
	repo := acquisitiontest.NewLedger()
	track := pendingTrack(retryTestUserId)
	repo.Seed(track)
	scheduler := &retryFakeScheduler{}
	h := download.NewRetryHandler(repo, scheduler, admitAlways{})
	router := chi.NewRouter()
	router.Use(signedInAs(retryTestUserId))
	router.Post("/tracks/{trackId}/retry", h.HandleRetryAcquisition)

	rec := retryServe(t, router, http.MethodPost, "/tracks/"+track.ID.UUID().String()+"/retry")

	retryAssertStatus(t, rec, http.StatusAccepted)
	if len(scheduler.scheduled) != 1 {
		t.Errorf("scheduled = %d tracks, want 1", len(scheduler.scheduled))
	}
}

func TestHandleReacquire_UsesTheInjectedAdmission(t *testing.T) {
	repo := acquisitiontest.NewLedger()
	track := pendingTrack(reacquireTestUserId)
	repo.Seed(track)
	scheduler := &reacquireFakeScheduler{}
	h := download.NewReacquireHandler(repo, scheduler, admitAlways{})
	router := chi.NewRouter()
	router.Use(signedInAs(reacquireTestUserId))
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

func (s *memCooldownStore) Reserve(_ context.Context, trackID ids.TrackId, kind download.CooldownKind, cooldown time.Duration) (time.Time, bool, time.Duration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now, key := s.now(), string(kind)+"/"+trackID.String()
	if last, ok := s.lastAt[key]; ok && now.Sub(last) < cooldown {
		return time.Time{}, false, cooldown - now.Sub(last), nil
	}
	s.lastAt[key] = now
	return now, true, 0, nil
}

func (s *memCooldownStore) Release(_ context.Context, trackID ids.TrackId, kind download.CooldownKind, at time.Time) error {
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
	reacquireTestUserId   = ids.NewUserId(reacquireTestUserUUID)
)

type reacquireFakeScheduler struct {
	replaced []ids.TrackId
	err      error
}

func (s *reacquireFakeScheduler) ScheduleReplace(_ context.Context, _ ids.UserId, trackId ids.TrackId) error {
	if s.err != nil {
		return s.err
	}
	s.replaced = append(s.replaced, trackId)
	return nil
}

func buildReacquireRouter(trackRepo *acquisitiontest.Ledger, scheduler *reacquireFakeScheduler) chi.Router {
	h := download.NewReacquireHandler(trackRepo, scheduler, download.NewReacquireAdmission(newMemCooldownStore()))
	r := chi.NewRouter()
	r.Use(signedInAs(reacquireTestUserId))
	r.Post("/tracks/{trackId}/reacquire", h.HandleReacquire)
	return r
}

func TestHandleReacquire(t *testing.T) {
	tests := []struct {
		name         string
		setup        func(*acquisitiontest.Ledger) string
		wantStatus   int
		wantReplaced bool
	}{
		{
			name: "streamable track returns 202 and schedules replace",
			setup: func(repo *acquisitiontest.Ledger) string {
				track := readyTrack(reacquireTestUserId)
				repo.Seed(track)
				return track.ID.UUID().String()
			},
			wantStatus:   http.StatusAccepted,
			wantReplaced: true,
		},
		{
			name: "pending track (no audio) returns 409",
			setup: func(repo *acquisitiontest.Ledger) string {
				track := pendingTrack(reacquireTestUserId)
				repo.Seed(track)
				return track.ID.UUID().String()
			},
			wantStatus: http.StatusConflict,
		},
		{
			name: "failed track (no audio) returns 409",
			setup: func(repo *acquisitiontest.Ledger) string {
				track := failedTrack(reacquireTestUserId)
				repo.Seed(track)
				return track.ID.UUID().String()
			},
			wantStatus: http.StatusConflict,
		},
		{
			name: "not found returns 404",
			setup: func(repo *acquisitiontest.Ledger) string {
				return uuid.New().String()
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "invalid track ID returns 400",
			setup: func(repo *acquisitiontest.Ledger) string {
				return "not-a-uuid"
			},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := acquisitiontest.NewLedger()
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
	repo := acquisitiontest.NewLedger()
	scheduler := &reacquireFakeScheduler{}
	track := readyTrack(reacquireTestUserId)
	repo.Seed(track)
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
	repo := acquisitiontest.NewLedger()
	scheduler := &reacquireFakeScheduler{}
	track := readyTrack(reacquireTestUserId)
	repo.Seed(track)
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
	repo := acquisitiontest.NewLedger()
	scheduler := &reacquireFakeScheduler{}
	router := buildReacquireRouter(repo, scheduler)

	req := httptest.NewRequest(http.MethodPost, "/tracks/"+uuid.New().String()+"/reacquire", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	retryAssertStatus(t, rec, http.StatusUnauthorized)
}

var (
	retryTestUserUUID = uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	retryTestUserId   = ids.NewUserId(retryTestUserUUID)
)

func signedInAs(userID ids.UserId) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "" {
				r = r.WithContext(requestuser.ContextWithUserID(r.Context(), userID))
			}
			next.ServeHTTP(w, r)
		})
	}
}

func pendingTrack(userId ids.UserId) acquisitiontest.LedgerTrack {
	return acquisitiontest.LedgerTrack{TrackSnapshot: download.TrackSnapshot{
		ID: ids.NewTrackId(), UserId: userId, Title: "Song", Artist: "Artist", Album: "Album", Status: download.TrackPending,
	}}
}

func failedTrack(userId ids.UserId) acquisitiontest.LedgerTrack {
	track := pendingTrack(userId)
	track.Status = download.TrackFailed
	return track
}

func readyTrack(userId ids.UserId) acquisitiontest.LedgerTrack {
	track := pendingTrack(userId)
	audioRef := "audio/ready.opus"
	track.AudioRef, track.Status = &audioRef, download.TrackReady
	return track
}

type retryFakeScheduler struct {
	scheduled []ids.TrackId
	err       error
}

func (s *retryFakeScheduler) Schedule(_ context.Context, _ ids.UserId, trackId ids.TrackId, _ string) error {
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

func buildRetryRouter(trackRepo *acquisitiontest.Ledger, scheduler *retryFakeScheduler) chi.Router {
	h := download.NewRetryHandler(trackRepo, scheduler, download.NewRetryAdmission(newMemCooldownStore()))
	r := chi.NewRouter()
	r.Use(signedInAs(retryTestUserId))
	r.Post("/tracks/{trackId}/retry", h.HandleRetryAcquisition)
	return r
}

func TestHandleRetryAcquisition(t *testing.T) {
	tests := []struct {
		name          string
		setup         func(*acquisitiontest.Ledger) string
		wantStatus    int
		wantScheduled bool
	}{
		{
			name: "failed track returns 202 and schedules retry",
			setup: func(repo *acquisitiontest.Ledger) string {
				track := failedTrack(retryTestUserId)
				repo.Seed(track)
				return track.ID.UUID().String()
			},
			wantStatus:    http.StatusAccepted,
			wantScheduled: true,
		},
		{
			name: "not found returns 404",
			setup: func(repo *acquisitiontest.Ledger) string {
				return uuid.New().String()
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "pending track (not failed) returns 409",
			setup: func(repo *acquisitiontest.Ledger) string {
				track := pendingTrack(retryTestUserId)
				repo.Seed(track)
				return track.ID.UUID().String()
			},
			wantStatus: http.StatusConflict,
		},
		{
			name: "ready track (not failed) returns 409",
			setup: func(repo *acquisitiontest.Ledger) string {
				track := readyTrack(retryTestUserId)
				repo.Seed(track)
				return track.ID.UUID().String()
			},
			wantStatus: http.StatusConflict,
		},
		{
			name: "invalid track ID returns 400",
			setup: func(repo *acquisitiontest.Ledger) string {
				return "not-a-uuid"
			},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := acquisitiontest.NewLedger()
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
	repo := acquisitiontest.NewLedger()
	scheduler := &retryFakeScheduler{}
	router := buildRetryRouter(repo, scheduler)

	req := httptest.NewRequest(http.MethodPost, "/tracks/"+uuid.New().String()+"/retry", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	retryAssertStatus(t, rec, http.StatusUnauthorized)
}

type ownedRoute struct {
	name       string
	path       string
	actionable func(ids.UserId) acquisitiontest.LedgerTrack
	mount      func(ledger *acquisitiontest.Ledger, cooldowns *memCooldownStore, as ids.UserId) (chi.Router, func() int)
}

func ownedRoutes() []ownedRoute {
	return []ownedRoute{
		{
			name:       "retry",
			path:       "/retry",
			actionable: failedTrack,
			mount: func(ledger *acquisitiontest.Ledger, cooldowns *memCooldownStore, as ids.UserId) (chi.Router, func() int) {
				scheduler := &retryFakeScheduler{}
				r := chi.NewRouter()
				r.Use(signedInAs(as))
				r.Post("/tracks/{trackId}/retry", download.NewRetryHandler(ledger, scheduler, download.NewRetryAdmission(cooldowns)).HandleRetryAcquisition)
				return r, func() int { return len(scheduler.scheduled) }
			},
		},
		{
			name:       "reacquire",
			path:       "/reacquire",
			actionable: readyTrack,
			mount: func(ledger *acquisitiontest.Ledger, cooldowns *memCooldownStore, as ids.UserId) (chi.Router, func() int) {
				scheduler := &reacquireFakeScheduler{}
				r := chi.NewRouter()
				r.Use(signedInAs(as))
				r.Post("/tracks/{trackId}/reacquire", download.NewReacquireHandler(ledger, scheduler, download.NewReacquireAdmission(cooldowns)).HandleReacquire)
				return r, func() int { return len(scheduler.replaced) }
			},
		},
	}
}

func TestHandlers_AnotherUsersTrackIsAnsweredExactlyLikeAMissingOne(t *testing.T) {
	for _, route := range ownedRoutes() {
		t.Run(route.name, func(t *testing.T) {
			owner, intruder := ids.NewUserId(uuid.New()), ids.NewUserId(uuid.New())
			track := route.actionable(owner)
			ledger := acquisitiontest.NewLedger(track)
			cooldowns := newMemCooldownStore()
			asIntruder, intruderScheduled := route.mount(ledger, cooldowns, intruder)

			missing := retryServe(t, asIntruder, http.MethodPost, "/tracks/"+uuid.New().String()+route.path)
			theirs := retryServe(t, asIntruder, http.MethodPost, "/tracks/"+track.ID.UUID().String()+route.path)

			retryAssertStatus(t, theirs, http.StatusNotFound)
			if theirs.Body.String() != missing.Body.String() || !reflect.DeepEqual(theirs.Header(), missing.Header()) {
				t.Errorf("another user's track answered %v %q, a missing track %v %q: the two must be indistinguishable",
					theirs.Header(), theirs.Body.String(), missing.Header(), missing.Body.String())
			}
			if n := intruderScheduled(); n != 0 {
				t.Errorf("another user's request scheduled %d jobs on the owner's track", n)
			}

			asOwner, ownerScheduled := route.mount(ledger, cooldowns, owner)
			retryAssertStatus(t, retryServe(t, asOwner, http.MethodPost, "/tracks/"+track.ID.UUID().String()+route.path), http.StatusAccepted)
			if n := ownerScheduled(); n != 1 {
				t.Errorf("the owner's request scheduled %d jobs, want 1: another user's request must not use up the owner's cooldown", n)
			}
		})
	}
}

func TestHandlers_RefusalsAreProblemDocumentsWithACode(t *testing.T) {
	routes := map[string]func() (chi.Router, string){
		"reacquire": func() (chi.Router, string) {
			return buildReacquireRouter(acquisitiontest.NewLedger(), &reacquireFakeScheduler{}), "reacquire"
		},
		"retry": func() (chi.Router, string) {
			return buildRetryRouter(acquisitiontest.NewLedger(), &retryFakeScheduler{}), "retry"
		},
	}
	refusals := map[string]struct {
		id         string
		wantStatus int
		wantCode   string
	}{
		"malformed id":  {"not-a-uuid", http.StatusBadRequest, "catalog.invalid_track_id"},
		"unknown track": {uuid.New().String(), http.StatusNotFound, "catalog.track_not_found"},
	}
	for routeName, build := range routes {
		for refusalName, want := range refusals {
			t.Run(routeName+"/"+refusalName, func(t *testing.T) {
				router, suffix := build()
				rec := retryServe(t, router, http.MethodPost, "/tracks/"+want.id+"/"+suffix)
				retryAssertStatus(t, rec, want.wantStatus)
				if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
					t.Errorf("Content-Type = %q, want application/problem+json", ct)
				}
				var body struct {
					Code string `json:"code"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Code != want.wantCode {
					t.Errorf("body %s: code %q (decode error %v), want %q", rec.Body.String(), body.Code, err, want.wantCode)
				}
			})
		}
	}
}
