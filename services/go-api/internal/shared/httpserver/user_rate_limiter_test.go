package httpserver_test

import (
	"altune/go-api/internal/shared/httpserver"
	"altune/go-api/internal/shared/ids"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type limiterClock struct{ t time.Time }

func newLimiterClock() *limiterClock { return &limiterClock{t: time.Unix(1_700_000_000, 0)} }

func (c *limiterClock) now() time.Time { return c.t }

func (c *limiterClock) advance(d time.Duration) { c.t = c.t.Add(d) }

type allower interface {
	Allow(key string) (bool, time.Duration)
}

func allowedRun(l allower, key string, requests int) []bool {
	got := make([]bool, requests)
	for i := range got {
		got[i], _ = l.Allow(key)
	}
	return got
}

func TestUserRateLimiter_AdmitsTheBurstThenRefusesWithTheRefillWait(t *testing.T) {
	l := httpserver.MustNewUserRateLimiter(httpserver.RateLimit{Every: 10 * time.Second, Burst: 2}, newLimiterClock().now)

	first, firstWait := l.Allow("user")
	second, _ := l.Allow("user")
	third, wait := l.Allow("user")

	if !first || firstWait != 0 || !second || third {
		t.Fatalf("burst of 2 admitted %v (wait %v), %v, %v, want true (0), true, false", first, firstWait, second, third)
	}
	if wait != 10*time.Second {
		t.Fatalf("refused wait = %v, want the 10s refill of one token", wait)
	}
}

func TestUserRateLimiter_ARefusalSpendsNoToken(t *testing.T) {
	clock := newLimiterClock()
	l := httpserver.MustNewUserRateLimiter(httpserver.RateLimit{Every: 10 * time.Second, Burst: 1}, clock.now)
	l.Allow("user")
	for range 5 {
		l.Allow("user")
	}

	clock.advance(10 * time.Second)

	if allowed, wait := l.Allow("user"); !allowed {
		t.Fatalf("after one refill period a refused caller got allowed=false wait=%v: refusals spent tokens", wait)
	}
}

func TestUserRateLimiter_KeepsEachKeysBudgetApart(t *testing.T) {
	l := httpserver.MustNewUserRateLimiter(httpserver.RateLimit{Every: time.Hour, Burst: 1}, newLimiterClock().now)
	l.Allow("a")

	if allowed, _ := l.Allow("b"); !allowed {
		t.Fatal("a second key was refused by the first key's spent bucket")
	}
}

func TestUserRateLimiter_KeepsBucketsThatAreStillSpent(t *testing.T) {
	clock := newLimiterClock()
	l := httpserver.MustNewUserRateLimiter(httpserver.RateLimit{Every: time.Minute, Burst: 1}, clock.now)

	l.Allow("other")
	clock.advance(50 * time.Second)
	l.Allow("spender")
	clock.advance(11 * time.Second)
	l.Allow("other")

	if allowed, _ := l.Allow("spender"); allowed {
		t.Fatal("a bucket surviving a reclaim with only 11s of a 60s refill must still be throttled")
	}
}

func TestUserRateLimiter_ReclaimsNoBucketBeforeItCouldHaveRefilled(t *testing.T) {
	clock := newLimiterClock()
	l := httpserver.MustNewUserRateLimiter(httpserver.RateLimit{Every: time.Minute, Burst: 2}, clock.now)
	allowedRun(l, "spender", 2)

	for range 2 {
		clock.advance(31 * time.Second)
		l.Allow("other")
	}
	clock.advance(3 * time.Second)

	if got := allowedRun(l, "spender", 2); !got[0] || got[1] {
		t.Fatalf("65s after spending a burst of 2 at one token a minute, admitted %v, want [true false]", got)
	}
}

func TestUserRateLimiter_ZeroBurstStillAdmitsOne(t *testing.T) {
	l := httpserver.MustNewUserRateLimiter(httpserver.RateLimit{Every: time.Hour}, time.Now)

	if allowed, _ := l.Allow("user"); !allowed {
		t.Fatal("a zero burst must be raised to one admitted request")
	}
	if allowed, wait := l.Allow("user"); allowed || wait <= 0 {
		t.Fatalf("second request allowed=%v wait=%v, want refused with a wait", allowed, wait)
	}
}

func TestMustNewUserRateLimiter_PanicsOnANilClock(t *testing.T) {
	defer func() {
		r := recover()
		err, isErr := r.(error)
		if !isErr || !strings.Contains(err.Error(), "the rate limiter requires a clock") {
			t.Fatalf("MustNewUserRateLimiter(nil clock) panicked with %v, want the clock-required error", r)
		}
	}()

	httpserver.MustNewUserRateLimiter(httpserver.RateLimit{Every: time.Hour, Burst: 1}, nil)

	t.Fatal("MustNewUserRateLimiter(nil clock) returned a limiter, want a panic")
}

func heapInUse() uint64 {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

func TestUserRateLimiter_ReclaimsIdleBucketsWithinTwoIdlePeriods(t *testing.T) {
	const idleBuckets = 100_000
	limit := httpserver.RateLimit{Every: time.Second, Burst: 2}
	idlePeriod := limit.Every * time.Duration(limit.Burst)
	clock := newLimiterClock()
	l := httpserver.MustNewUserRateLimiter(limit, clock.now)
	l.Allow("fresh")
	empty := heapInUse()
	for range idleBuckets {
		l.Allow(uuid.NewString())
	}
	held := heapInUse()

	for range 2 {
		clock.advance(idlePeriod)
		l.Allow("fresh")
	}
	reclaimed := heapInUse()
	runtime.KeepAlive(l)

	grown := int64(held) - int64(empty)
	if grown < 10<<20 {
		t.Fatalf("%d idle buckets grew the heap by only %d bytes, the measurement cannot tell", idleBuckets, grown)
	}
	if left := int64(reclaimed) - int64(empty); left > grown/4 {
		t.Fatalf("after two idle periods %d of %d bytes are still held, want the idle buckets reclaimed", left, grown)
	}
}

func rotatingCallDuration(idleBuckets int) time.Duration {
	limit := httpserver.RateLimit{Every: time.Second, Burst: 2}
	clock := newLimiterClock()
	l := httpserver.MustNewUserRateLimiter(limit, clock.now)
	l.Allow("caller")
	for i := range idleBuckets {
		l.Allow(strconv.Itoa(i))
	}
	clock.advance(limit.Every * time.Duration(limit.Burst))
	start := time.Now()
	l.Allow("caller")
	return time.Since(start)
}

func fastestRotatingCall(idleBuckets, trials int) time.Duration {
	fastest := time.Duration(1<<63 - 1)
	for range trials {
		fastest = min(fastest, rotatingCallDuration(idleBuckets))
	}
	return fastest
}

func TestUserRateLimiter_ReclaimingIdleBucketsDoesNotStallTheCaller(t *testing.T) {
	small := fastestRotatingCall(500, 3)
	large := fastestRotatingCall(50_000, 3)

	if large > 10*max(small, time.Microsecond) {
		t.Fatalf("the call that reclaims 50k idle buckets took %v against %v for 500, want the reclaim not to scale with idle buckets", large, small)
	}
}

func TestUserRateLimiter_MiddlewareRefusesWithRetryAfter(t *testing.T) {
	clock := newLimiterClock()
	l := httpserver.MustNewUserRateLimiter(httpserver.RateLimit{Every: 2 * time.Second, Burst: 1}, clock.now)
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
	if second.Code == http.StatusNoContent {
		t.Fatal("the refused request reached the handler")
	}
}

func TestUserRateLimiter_MiddlewareRefusesWithoutARefusalHook(t *testing.T) {
	l := httpserver.MustNewUserRateLimiter(httpserver.RateLimit{Every: time.Hour, Burst: 1}, newLimiterClock().now)
	handler := l.Middleware(func(*http.Request) (string, bool) { return "user", true }, errors.New("refused"), nil)(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	refused := httptest.NewRecorder()
	handler.ServeHTTP(refused, httptest.NewRequest(http.MethodGet, "/", nil))

	if refused.Code == http.StatusNoContent || refused.Header().Get("Retry-After") != "3600" {
		t.Fatalf("second request = %d retry-after %q, want refused with Retry-After 3600", refused.Code, refused.Header().Get("Retry-After"))
	}
}

type limiterCtxKey struct{}

func TestUserKey_ReadsTheUserFromTheRequestContext(t *testing.T) {
	id := ids.NewUserId(uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"))
	fromContext := func(ctx context.Context) (ids.UserId, bool) { return id, ctx.Value(limiterCtxKey{}) != nil }
	key := httpserver.UserKey(fromContext)

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
	l := httpserver.MustNewUserRateLimiter(httpserver.RateLimit{Every: time.Hour, Burst: 1}, time.Now)
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
