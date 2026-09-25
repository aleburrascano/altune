package handler

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/discovery/service"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/httputil"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	discdomain "altune/go-api/internal/discovery/domain"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func TestSearchRateLimit_RapidSearchesAreThrottled(t *testing.T) {
	router := buildDiscoveryRouter(&fakeSearchProvider{name: discdomain.ProviderDeezer}, nil, nil, nil)

	throttled := 0
	for range 200 {
		rec := discServe(t, router, http.MethodGet, "/discovery/search?q=radiohead&save_history=false", nil)
		if rec.Code == http.StatusTooManyRequests {
			throttled++
		}
	}
	if throttled == 0 {
		t.Fatal("200 rapid /search requests from one user: none throttled")
	}
}

func TestSearchRateLimit_DefaultBudgetsThenRejects(t *testing.T) {
	cases := []struct {
		name   string
		router chi.Router
		path   string
		max    int
	}{
		{
			name:   "search",
			router: buildDiscoveryRouter(&fakeSearchProvider{name: discdomain.ProviderDeezer}, nil, nil, nil),
			path:   "/discovery/search?q=radiohead&save_history=false",
			max:    DefaultDiscoveryRateLimits.Search.Max,
		},
		{
			name:   "suggest",
			router: buildSuggestRouter(&fakeVocabStore{}),
			path:   "/discovery/suggest?q=radio",
			max:    DefaultDiscoveryRateLimits.Suggest.Max,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for i := range tc.max {
				rec := discServe(t, tc.router, http.MethodGet, tc.path, nil)
				if rec.Code == http.StatusTooManyRequests {
					t.Fatalf("request %d of %d throttled inside the budget", i+1, tc.max)
				}
			}

			rec := discServe(t, tc.router, http.MethodGet, tc.path, nil)
			assertRateLimited(t, rec)
		})
	}
}

func TestContentRateLimit_RapidLyricsRequestsAreThrottled(t *testing.T) {
	router := buildDiscoveryRouter(nil, nil, nil, nil)

	var throttled *httptest.ResponseRecorder
	for range 200 {
		rec := discServe(t, router, http.MethodGet, "/discovery/lyrics?title=paranoid+android", nil)
		if rec.Code == http.StatusTooManyRequests {
			throttled = rec
		}
	}

	if throttled == nil {
		t.Fatal("200 rapid /lyrics requests from one user: none throttled")
	}
	assertRateLimited(t, throttled)
}

func TestContentRateLimit_OneBudgetSpansEveryContentRoute(t *testing.T) {
	router := buildRateLimitedRouter(DiscoveryRateLimits{
		Search:  RequestLimit{Max: 10, Window: time.Hour},
		Content: RequestLimit{Max: 2, Window: time.Hour},
	})

	for range 2 {
		discAssertStatus(t, discServe(t, router, http.MethodGet, "/discovery/lyrics?title=a", nil), http.StatusOK)
	}

	assertRateLimited(t, discServe(t, router, http.MethodGet, "/discovery/enrichment?kind=track&title=a", nil))
	discAssertStatus(t, discServe(t, router, http.MethodGet, "/discovery/search?q=a&save_history=false", nil), http.StatusOK)
}

func TestEventRateLimit_DefaultBudgetThenRejects(t *testing.T) {
	router := buildEventRouter(&recordingEventStore{})
	body := discJsonBody(t, map[string]any{"type": "play"}).String()

	for i := range DefaultDiscoveryRateLimits.Events.Max {
		rec := discServe(t, router, http.MethodPost, "/discovery/events", strings.NewReader(body))
		if rec.Code == http.StatusTooManyRequests {
			t.Fatalf("request %d of %d throttled inside the budget", i+1, DefaultDiscoveryRateLimits.Events.Max)
		}
	}

	assertRateLimited(t, discServe(t, router, http.MethodPost, "/discovery/events", strings.NewReader(body)))
}

func TestSearchRateLimit_BudgetsArePerUserAndPerRoute(t *testing.T) {
	h := NewDiscoveryHandler(DiscoveryServices{
		Search:  service.NewService(nil, service.NewCircuitBreaker()),
		Suggest: service.NewSuggestService(&fakeVocabStore{}),
	}).WithRateLimits(DiscoveryRateLimits{
		Search:  RequestLimit{Max: 2, Window: time.Hour},
		Suggest: RequestLimit{Max: 2, Window: time.Hour},
	})
	r := chi.NewRouter()
	r.Use(auth.Middleware(auth.VerifierFunc(func(_ context.Context, token string) (auth.VerifiedToken, error) {
		if token == "other" {
			return auth.VerifiedToken{UserID: otherTestUserId, ExpiresAt: time.Now().Add(time.Hour)}, nil
		}
		return auth.VerifiedToken{UserID: discTestUserId, ExpiresAt: time.Now().Add(time.Hour)}, nil
	})))
	r.Mount("/discovery", h.Routes())

	for range 2 {
		discAssertStatus(t, discServe(t, r, http.MethodGet, "/discovery/search?q=a&save_history=false", nil), http.StatusOK)
	}
	assertRateLimited(t, discServe(t, r, http.MethodGet, "/discovery/search?q=a&save_history=false", nil))

	discAssertStatus(t, discServe(t, r, http.MethodGet, "/discovery/suggest?q=a", nil), http.StatusOK)

	rec := serveAs(t, r, "other", "/discovery/search?q=a&save_history=false")
	discAssertStatus(t, rec, http.StatusOK)
}

func TestSearchRateLimit_UnauthenticatedStillGets401(t *testing.T) {
	h := NewDiscoveryHandler(DiscoveryServices{}).WithRateLimits(DiscoveryRateLimits{
		Search:  RequestLimit{Max: 0, Window: time.Hour},
		Suggest: RequestLimit{Max: 0, Window: time.Hour},
	})
	r := chi.NewRouter()
	r.Mount("/discovery", h.Routes())

	discAssertStatus(t, discServeNoAuth(t, r, http.MethodGet, "/discovery/search?q=a"), http.StatusUnauthorized)
}

func TestUserRateLimiter_WindowSlidesAndIdleUsersArePruned(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	l := newUserRateLimiter(RequestLimit{Max: 2, Window: time.Minute}, func() time.Time { return now })

	mustAdmit(t, l, "u1")
	now = now.Add(20 * time.Second)
	mustAdmit(t, l, "u1")

	now = now.Add(10 * time.Second)
	wait, ok := l.admit("u1")
	if ok {
		t.Fatal("third request inside the window was admitted")
	}
	if wait != 30*time.Second {
		t.Fatalf("wait = %v, want 30s (until the first request leaves the window)", wait)
	}
	if got := retryAfterSeconds(wait); got != 30 {
		t.Fatalf("Retry-After = %d, want 30", got)
	}

	now = now.Add(30 * time.Second)
	mustAdmit(t, l, "u1")

	mustAdmit(t, l, "u2")
	now = now.Add(2 * time.Minute)
	mustAdmit(t, l, "u3")
	if _, kept := l.logs["u2"]; kept {
		t.Fatal("idle user u2 was not pruned after its window passed")
	}
	if len(l.logs) != 1 {
		t.Fatalf("logs holds %d users, want only the active u3", len(l.logs))
	}
}

func TestRetryAfterSeconds_RoundsUpToAtLeastOne(t *testing.T) {
	for wait, want := range map[time.Duration]int{
		0:                       1,
		time.Millisecond:        1,
		time.Second:             1,
		1500 * time.Millisecond: 2,
	} {
		if got := retryAfterSeconds(wait); got != want {
			t.Errorf("retryAfterSeconds(%v) = %d, want %d", wait, got, want)
		}
	}
}

var otherTestUserId = shared.NewUserId(uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"))

func buildRateLimitedRouter(limits DiscoveryRateLimits) chi.Router {
	h := NewDiscoveryHandler(DiscoveryServices{
		Search: service.NewService(nil, service.NewCircuitBreaker()),
	}).WithRateLimits(limits)
	r := chi.NewRouter()
	r.Use(auth.Middleware(discVerifyAsTestUser))
	r.Mount("/discovery", h.Routes())
	return r
}

func mustAdmit(t *testing.T, l *userRateLimiter, key string) {
	t.Helper()
	if _, ok := l.admit(key); !ok {
		t.Fatalf("request for %s rejected inside its budget", key)
	}
}

func serveAs(t *testing.T, router chi.Router, token, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func assertRateLimited(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	discAssertStatus(t, rec, http.StatusTooManyRequests)
	if rec.Header().Get("Retry-After") == "" {
		t.Error("throttled response has no Retry-After header")
	}
	var resp httputil.ErrorResponse
	discDecodeJSON(t, rec, &resp)
	if resp.Code != "discovery.rate_limited" {
		t.Errorf("code = %q, want discovery.rate_limited", resp.Code)
	}
}
