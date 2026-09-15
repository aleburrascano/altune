package handler

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/playback/domain"
	"altune/go-api/internal/playback/service"
	"altune/go-api/internal/shared"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

const validSaveBody = `{"track_ids":[],"repeat_mode":"off","source_id":"library"}`

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock() *fakeClock { return &fakeClock{t: time.Unix(1_700_000_000, 0)} }

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// countingRepo counts upserts so a test can prove a throttled request never
// reached Postgres.
type countingRepo struct {
	recordingRepo
	mu      sync.Mutex
	upserts int
}

func (r *countingRepo) Upsert(ctx context.Context, state *domain.QueueState) error {
	r.mu.Lock()
	r.upserts++
	r.mu.Unlock()
	return r.recordingRepo.Upsert(ctx, state)
}

func requestAs(method string, user shared.UserId, body string) *http.Request {
	req := httptest.NewRequest(method, "/queue-state", strings.NewReader(body))
	return req.WithContext(auth.ContextWithUserID(req.Context(), user))
}

func serve(h *QueueHandler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.Routes().ServeHTTP(rec, req)
	return rec
}

func TestQueueStateRateLimit_RapidPutsFromOnePrincipalAreThrottled(t *testing.T) {
	// Reproduces #1123: N rapid PUT /queue-state calls from one authenticated
	// principal all used to succeed, each one an upsert on the shared pool.
	// Once the burst is spent the next call must be a 429 that never writes.
	clock := newFakeClock()
	repo := &countingRepo{}
	limit := QueueStateRateLimit{Every: 2 * time.Second, Burst: 5}
	h := NewQueueHandler(service.NewQueueService(repo, nilNowPlaying{}), WithQueueStateRateLimit(limit), withClock(clock.now))
	user := shared.NewUserId(uuid.New())

	for i := range limit.Burst {
		if rec := serve(h, requestAs(http.MethodPut, user, validSaveBody)); rec.Code != http.StatusNoContent {
			t.Fatalf("request %d inside the burst must succeed, got %d body %q", i, rec.Code, rec.Body.String())
		}
	}

	const flood = 50
	for i := range flood {
		rec := serve(h, requestAs(http.MethodPut, user, validSaveBody))
		if rec.Code != http.StatusTooManyRequests {
			t.Fatalf("flood request %d must be throttled with 429, got %d", i, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), `"code":"playback.rate_limited"`) {
			t.Fatalf("throttled body must carry the rate-limit code, got %q", rec.Body.String())
		}
		if got := rec.Header().Get("Retry-After"); got != "2" {
			t.Fatalf("Retry-After must tell the client when a token frees up, got %q", got)
		}
	}
	if repo.upserts != limit.Burst {
		t.Fatalf("throttled requests must not reach the repository: %d upserts, want %d", repo.upserts, limit.Burst)
	}

	clock.advance(limit.Every)
	if rec := serve(h, requestAs(http.MethodPut, user, validSaveBody)); rec.Code != http.StatusNoContent {
		t.Fatalf("a refilled token must admit the next save, got %d", rec.Code)
	}
}

func TestQueueStateRateLimit_IsPerPrincipal(t *testing.T) {
	clock := newFakeClock()
	h := NewQueueHandler(service.NewQueueService(&recordingRepo{}, nilNowPlaying{}),
		WithQueueStateRateLimit(QueueStateRateLimit{Every: time.Minute, Burst: 1}), withClock(clock.now))
	noisy, quiet := shared.NewUserId(uuid.New()), shared.NewUserId(uuid.New())

	serve(h, requestAs(http.MethodPut, noisy, validSaveBody))
	if rec := serve(h, requestAs(http.MethodPut, noisy, validSaveBody)); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("noisy user must be throttled, got %d", rec.Code)
	}
	if rec := serve(h, requestAs(http.MethodPut, quiet, validSaveBody)); rec.Code != http.StatusNoContent {
		t.Fatalf("another user's budget must be untouched, got %d", rec.Code)
	}
}

func TestQueueStateRateLimit_DefaultAdmitsAutosaveCadence(t *testing.T) {
	// The mobile client saves every 15s and again on each background/inactive
	// transition. Three signed-in devices doing that for an hour, each also
	// flapping to the background every 30s, must never see a 429.
	clock := newFakeClock()
	h := NewQueueHandler(service.NewQueueService(&recordingRepo{}, nilNowPlaying{}), withClock(clock.now))
	user := shared.NewUserId(uuid.New())

	for range 3 {
		if rec := serve(h, requestAs(http.MethodGet, user, "")); rec.Code != http.StatusOK {
			t.Fatalf("resume GET must succeed, got %d", rec.Code)
		}
	}
	for tick := range 240 {
		saves := 3
		if tick%2 == 0 {
			saves += 3
		}
		for range saves {
			if rec := serve(h, requestAs(http.MethodPut, user, validSaveBody)); rec.Code != http.StatusNoContent {
				t.Fatalf("legitimate autosave at tick %d throttled: %d", tick, rec.Code)
			}
		}
		clock.advance(15 * time.Second)
	}
}

func TestQueueStateRateLimit_ForgetIsNeverThrottled(t *testing.T) {
	clock := newFakeClock()
	h := NewQueueHandler(service.NewQueueService(&recordingRepo{}, nilNowPlaying{}),
		WithQueueStateRateLimit(QueueStateRateLimit{Every: time.Hour, Burst: 1}), withClock(clock.now))
	user := shared.NewUserId(uuid.New())

	serve(h, requestAs(http.MethodPut, user, validSaveBody))
	if rec := serve(h, requestAs(http.MethodGet, user, "")); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("GET shares the PUT bucket and must be throttled, got %d", rec.Code)
	}
	if rec := serve(h, requestAs(http.MethodDelete, user, "")); rec.Code != http.StatusNoContent {
		t.Fatalf("GDPR erasure must never be throttled, got %d", rec.Code)
	}
}

func TestQueueStateRateLimit_UnauthenticatedStillGets401(t *testing.T) {
	h := NewQueueHandler(service.NewQueueService(&recordingRepo{}, nilNowPlaying{}))
	req := httptest.NewRequest(http.MethodPut, "/queue-state", strings.NewReader(validSaveBody))

	if rec := serve(h, req); rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing principal must still be a 401, got %d", rec.Code)
	}
}

func TestUserRateLimiter_EvictsRefilledBuckets(t *testing.T) {
	clock := newFakeClock()
	limit := QueueStateRateLimit{Every: time.Second, Burst: 2}
	l := newUserRateLimiter(limit, clock.now)

	for range 1000 {
		l.allow(uuid.NewString())
	}
	clock.advance(limit.Every * time.Duration(limit.Burst))
	l.allow("fresh")

	if got := len(l.buckets); got != 1 {
		t.Fatalf("idle refilled buckets must be evicted, %d remain", got)
	}
}
