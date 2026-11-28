package handler_test

import (
	"altune/go-api/internal/playback/internal/adapters/handler"
	"altune/go-api/internal/playback/internal/domain"
	"altune/go-api/internal/playback/internal/ports"
	"altune/go-api/internal/playback/internal/service"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/requestuser"
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

type countingRateLimitMetrics struct {
	mu       sync.Mutex
	refusals int
}

var _ ports.RateLimitMetrics = (*countingRateLimitMetrics)(nil)

func (m *countingRateLimitMetrics) QueueStateRateLimited() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.refusals++
}

func (m *countingRateLimitMetrics) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.refusals
}

func requestAs(method string, user ids.UserId, body string) *http.Request {
	req := httptest.NewRequest(method, "/queue-state", strings.NewReader(body))
	return req.WithContext(requestuser.ContextWithUserID(req.Context(), user))
}

func serve(h *handler.QueueHandler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.Routes().ServeHTTP(rec, req)
	return rec
}

func TestQueueStateRateLimit_RapidPutsFromOnePrincipalAreThrottled(t *testing.T) {
	clock := newFakeClock()
	repo := &countingRepo{}
	limit := handler.QueueSaveLimit{Every: 2 * time.Second, Burst: 5}
	h := handler.MustNewQueueHandler(service.MustNewQueueService(repo, absentNowPlaying{}, time.Now), limit, clock.now, noopRateLimitMetrics{})
	user := ids.NewUserId(uuid.New())

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

func TestQueueStateRateLimit_CountsEveryRefusal(t *testing.T) {
	clock := newFakeClock()
	metrics := &countingRateLimitMetrics{}
	limit := handler.QueueSaveLimit{Every: 2 * time.Second, Burst: 3}
	h := handler.MustNewQueueHandler(service.MustNewQueueService(&recordingRepo{}, absentNowPlaying{}, time.Now), limit, clock.now, metrics)
	user := ids.NewUserId(uuid.New())

	for range limit.Burst {
		serve(h, requestAs(http.MethodPut, user, validSaveBody))
	}
	if got := metrics.count(); got != 0 {
		t.Fatalf("admitted requests must not count as refusals, got %d", got)
	}

	const flood = 7
	for i := range flood {
		if rec := serve(h, requestAs(http.MethodPut, user, validSaveBody)); rec.Code != http.StatusTooManyRequests {
			t.Fatalf("flood request %d must be refused, got %d", i, rec.Code)
		}
	}

	if got := metrics.count(); got != flood {
		t.Errorf("refusals counted %d, want one per 429 (%d)", got, flood)
	}
}

func TestQueueStateRateLimit_IsPerPrincipal(t *testing.T) {
	clock := newFakeClock()
	h := handler.MustNewQueueHandler(service.MustNewQueueService(&recordingRepo{}, absentNowPlaying{}, time.Now), handler.QueueSaveLimit{Every: time.Minute, Burst: 1}, clock.now, noopRateLimitMetrics{})
	noisy, quiet := ids.NewUserId(uuid.New()), ids.NewUserId(uuid.New())

	serve(h, requestAs(http.MethodPut, noisy, validSaveBody))
	if rec := serve(h, requestAs(http.MethodPut, noisy, validSaveBody)); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("noisy user must be throttled, got %d", rec.Code)
	}
	if rec := serve(h, requestAs(http.MethodPut, quiet, validSaveBody)); rec.Code != http.StatusNoContent {
		t.Fatalf("another user's budget must be untouched, got %d", rec.Code)
	}
}

func TestQueueStateRateLimit_DefaultAdmitsAutosaveCadence(t *testing.T) {
	clock := newFakeClock()
	h := handler.MustNewQueueHandler(service.MustNewQueueService(&recordingRepo{}, absentNowPlaying{}, time.Now), handler.DefaultQueueSaveLimit(), clock.now, noopRateLimitMetrics{})
	user := ids.NewUserId(uuid.New())

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
	h := handler.MustNewQueueHandler(service.MustNewQueueService(&recordingRepo{}, absentNowPlaying{}, time.Now), handler.QueueSaveLimit{Every: time.Hour, Burst: 1}, clock.now, noopRateLimitMetrics{})
	user := ids.NewUserId(uuid.New())

	serve(h, requestAs(http.MethodPut, user, validSaveBody))
	if rec := serve(h, requestAs(http.MethodGet, user, "")); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("GET shares the PUT bucket and must be throttled, got %d", rec.Code)
	}
	if rec := serve(h, requestAs(http.MethodDelete, user, "")); rec.Code != http.StatusNoContent {
		t.Fatalf("GDPR erasure must never be throttled, got %d", rec.Code)
	}
}

func TestQueueStateRateLimit_RefusalWireFormat(t *testing.T) {
	clock := newFakeClock()
	limit := handler.QueueSaveLimit{Every: 2 * time.Second, Burst: 1}
	h := handler.MustNewQueueHandler(service.MustNewQueueService(&recordingRepo{}, absentNowPlaying{}, time.Now), limit, clock.now, noopRateLimitMetrics{})
	user := ids.NewUserId(uuid.New())

	if rec := serve(h, requestAs(http.MethodPut, user, validSaveBody)); rec.Code != http.StatusNoContent {
		t.Fatalf("the request using the last token of the burst must get through, got %d", rec.Code)
	}

	rec := serve(h, requestAs(http.MethodPut, user, validSaveBody))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got == "" {
		t.Error("missing Retry-After header")
	}
	const want = `{"type":"urn:altune:problem:playback.rate_limited","title":"Too many queue state requests","status":429,"detail":"too many queue state requests, try again later","code":"playback.rate_limited"}`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Errorf("body = %s, want %s", got, want)
	}

	clock.advance(limit.Every)
	if rec := serve(h, requestAs(http.MethodPut, user, validSaveBody)); rec.Code != http.StatusNoContent {
		t.Fatalf("after Every the same user must be allowed again, got %d", rec.Code)
	}
}
