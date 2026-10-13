package httputil

import (
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
)

type limiterClock struct{ t time.Time }

func newLimiterClock() *limiterClock { return &limiterClock{t: time.Unix(1_700_000_000, 0)} }

func (c *limiterClock) now() time.Time { return c.t }

func (c *limiterClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func TestUserRateLimiter_ReclaimingIdleBucketsDoesNotStallOtherCallers(t *testing.T) {
	for _, idleBuckets := range []int{1_000, 250_000} {
		t.Run(strconv.Itoa(idleBuckets), func(t *testing.T) {
			clock := newLimiterClock()
			l := NewUserRateLimiter(RateLimit{Every: time.Second, Burst: 2}, clock.now)
			for i := range idleBuckets {
				l.Allow(strconv.Itoa(i))
			}
			clock.advance(l.idleTTL())

			l.Allow("caller")

			if len(l.active) != 1 {
				t.Fatalf("rotation must start a fresh active map, got %d entries", len(l.active))
			}
			if len(l.cooling) != idleBuckets {
				t.Fatalf("rotation must hand the old map over untouched, cooling has %d of %d buckets", len(l.cooling), idleBuckets)
			}
		})
	}
}

func TestUserRateLimiter_EvictsRefilledBuckets(t *testing.T) {
	clock := newLimiterClock()
	limit := RateLimit{Every: time.Second, Burst: 2}
	l := NewUserRateLimiter(limit, clock.now)

	for range 1000 {
		l.Allow(uuid.NewString())
	}
	for range 2 {
		clock.advance(l.idleTTL())
		l.Allow("fresh")
	}

	if got := len(l.active) + len(l.cooling); got != 1 {
		t.Fatalf("idle refilled buckets must be reclaimed within two idle TTLs, %d remain", got)
	}
}

func TestUserRateLimiter_KeepsBucketsThatAreStillSpent(t *testing.T) {
	clock := newLimiterClock()
	l := NewUserRateLimiter(RateLimit{Every: time.Minute, Burst: 1}, clock.now)

	l.Allow("other")
	clock.advance(50 * time.Second)
	l.Allow("spender")
	clock.advance(11 * time.Second)
	l.Allow("other")

	if allowed, _ := l.Allow("spender"); allowed {
		t.Fatal("a bucket surviving a reclaim with only 11s of a 60s refill must still be throttled")
	}
}

func TestUserRateLimiter_ZeroBurstStillAdmitsOne(t *testing.T) {
	l := NewUserRateLimiter(RateLimit{Every: time.Hour}, nil)

	if allowed, _ := l.Allow("user"); !allowed {
		t.Fatal("a zero burst must be raised to one admitted request")
	}
	if allowed, wait := l.Allow("user"); allowed || wait <= 0 {
		t.Fatalf("second request allowed=%v wait=%v, want refused with a wait", allowed, wait)
	}
}

func TestUserRateLimiter_MiddlewareRefusesWithRetryAfter(t *testing.T) {
	clock := newLimiterClock()
	l := NewUserRateLimiter(RateLimit{Every: 2 * time.Second, Burst: 1}, clock.now)
	refusals := 0
	handler := l.Middleware(func(*http.Request) (string, bool) { return "user", true },
		errors.New("refused"), func() { refusals++ })(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/", nil))
	second := httptest.NewRecorder()
	handler.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/", nil))

	if first.Code != http.StatusNoContent || second.Header().Get("Retry-After") != "2" || refusals != 1 {
		t.Fatalf("first=%d retry-after=%q refusals=%d, want 204, 2, 1", first.Code, second.Header().Get("Retry-After"), refusals)
	}
}

type limiterCtxKey struct{}

func TestUserKey_ReadsTheUserFromTheRequestContext(t *testing.T) {
	id := shared.NewUserId(uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"))
	fromContext := func(ctx context.Context) (shared.UserId, bool) { return id, ctx.Value(limiterCtxKey{}) != nil }
	key := UserKey(fromContext)

	signedIn := httptest.NewRequest(http.MethodGet, "/", nil)
	signedIn = signedIn.WithContext(context.WithValue(signedIn.Context(), limiterCtxKey{}, true))
	got, ok := key(signedIn)
	if !ok || got != id.String() {
		t.Fatalf("signed-in key = %q, %v, want %q, true", got, ok, id.String())
	}
	if _, ok := key(httptest.NewRequest(http.MethodGet, "/", nil)); ok {
		t.Fatal("anonymous request produced a key")
	}
}

func TestUserRateLimiter_MiddlewarePassesAnonymousRequests(t *testing.T) {
	l := NewUserRateLimiter(RateLimit{Every: time.Hour, Burst: 1}, nil)
	handler := l.Middleware(func(*http.Request) (string, bool) { return "", false }, errors.New("refused"), nil)(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))

	for i := range 3 {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		if rec.Code != http.StatusNoContent {
			t.Fatalf("anonymous request %d = %d, want 204", i, rec.Code)
		}
	}
}
