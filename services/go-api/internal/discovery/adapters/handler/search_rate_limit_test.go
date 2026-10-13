package handler

import (
	"altune/go-api/internal/auth/adapters/middleware"
	"altune/go-api/internal/discovery/service/favorites"
	"altune/go-api/internal/discovery/service/history"
	"altune/go-api/internal/discovery/service/providercall"
	"altune/go-api/internal/discovery/service/search"
	"altune/go-api/internal/discovery/service/vocabulary"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/httputil"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	authdomain "altune/go-api/internal/auth/domain"
	authports "altune/go-api/internal/auth/ports"
	authservice "altune/go-api/internal/auth/service"

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
		Search:  search.NewService(nil, providercall.NewCircuitBreaker()),
		Suggest: vocabulary.NewSuggestService(&fakeVocabStore{}),
	}).WithRateLimits(DiscoveryRateLimits{
		Search:  RequestLimit{Max: 2, Window: time.Hour},
		Suggest: RequestLimit{Max: 2, Window: time.Hour},
	})
	r := chi.NewRouter()
	r.Use(middleware.Middleware(authports.VerifierFunc(func(_ context.Context, token string) (authdomain.VerifiedToken, error) {
		if token == "other" {
			return authdomain.VerifiedToken{UserID: otherTestUserId, ExpiresAt: time.Now().Add(time.Hour)}, nil
		}
		return authdomain.VerifiedToken{UserID: discTestUserId, ExpiresAt: time.Now().Add(time.Hour)}, nil
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

func TestRequestLimit_BucketAdmitsBurstThenRefillsAtTheWindowRate(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	limiter := newUserRateLimiter(RequestLimit{Max: 2, Window: time.Minute}, func() time.Time { return now })
	serve := func() int {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req = req.WithContext(authservice.ContextWithUserID(req.Context(), otherTestUserId))
		rec := httptest.NewRecorder()
		limiter(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })).ServeHTTP(rec, req)
		return rec.Code
	}

	for i := range 2 {
		if got := serve(); got != http.StatusNoContent {
			t.Fatalf("burst request %d = %d, want 204", i+1, got)
		}
	}
	if got := serve(); got != http.StatusTooManyRequests {
		t.Fatalf("request past the burst = %d, want 429", got)
	}
	now = now.Add(30 * time.Second)
	if got := serve(); got != http.StatusNoContent {
		t.Fatalf("after Window/Max the bucket refilled one token, got %d", got)
	}
	if got := serve(); got != http.StatusTooManyRequests {
		t.Fatalf("only one token refilled, second request = %d, want 429", got)
	}
}

func TestRetryAfterSeconds_RoundsUpToAtLeastOne(t *testing.T) {
	for wait, want := range map[time.Duration]string{
		0:                       "1",
		time.Millisecond:        "1",
		time.Second:             "1",
		1500 * time.Millisecond: "2",
	} {
		if got := httputil.RetryAfterSeconds(wait); got != want {
			t.Errorf("RetryAfterSeconds(%v) = %q, want %q", wait, got, want)
		}
	}
}

var otherTestUserId = shared.NewUserId(uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"))

func buildRateLimitedRouter(limits DiscoveryRateLimits) chi.Router {
	h := NewDiscoveryHandler(DiscoveryServices{
		Search: search.NewService(nil, providercall.NewCircuitBreaker()),
	}).WithRateLimits(limits)
	r := chi.NewRouter()
	r.Use(middleware.Middleware(discVerifyAsTestUser))
	r.Mount("/discovery", h.Routes())
	return r
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

func TestUnmeteredUserRoutes_AreThrottledPerUser(t *testing.T) {
	cases := []struct {
		name   string
		method string
		path   string
	}{
		{"list history", http.MethodGet, "/discovery/search-history"},
		{"clear history", http.MethodDelete, "/discovery/search-history"},
		{"list favorites", http.MethodGet, "/discovery/favorites"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			historyRepo := &fakeSearchHistoryRepo{}
			h := NewDiscoveryHandler(DiscoveryServices{
				History:      history.NewListSearchHistoryService(historyRepo),
				ClearHistory: history.NewClearSearchHistoryService(historyRepo),
				Favorites:    favorites.NewFavoritesService(erroringFavoritesRepo{}),
			}).WithRateLimits(DiscoveryRateLimits{
				History:   RequestLimit{Max: 1, Window: time.Hour},
				Favorites: RequestLimit{Max: 1, Window: time.Hour},
			})
			r := chi.NewRouter()
			r.Use(middleware.Middleware(authports.VerifierFunc(func(_ context.Context, token string) (authdomain.VerifiedToken, error) {
				id := discTestUserId
				if token == "other" {
					id = otherTestUserId
				}
				return authdomain.VerifiedToken{UserID: id, ExpiresAt: time.Now().Add(time.Hour)}, nil
			})))
			r.Mount("/discovery", h.Routes())

			serve := func(token string) *httptest.ResponseRecorder {
				req := httptest.NewRequest(tc.method, tc.path, nil)
				req.Header.Set("Authorization", "Bearer "+token)
				rec := httptest.NewRecorder()
				r.ServeHTTP(rec, req)
				return rec
			}

			if rec := serve("first"); rec.Code == http.StatusTooManyRequests {
				t.Fatal("first request throttled inside the budget")
			}
			assertRateLimited(t, serve("first"))
			if rec := serve("other"); rec.Code == http.StatusTooManyRequests {
				t.Fatal("second user throttled by the first user's budget")
			}
		})
	}
}

func TestSearchRateLimit_AdmitsAgainAfterWindowResets(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	h := NewDiscoveryHandler(DiscoveryServices{
		Search: search.NewService(nil, providercall.NewCircuitBreaker()),
	}).WithRateLimits(DiscoveryRateLimits{Search: RequestLimit{Max: 1, Window: time.Minute}})
	h.searchLimiter = newUserRateLimiter(RequestLimit{Max: 1, Window: time.Minute}, func() time.Time { return now })
	r := chi.NewRouter()
	r.Use(middleware.Middleware(discVerifyAsTestUser))
	r.Mount("/discovery", h.Routes())
	const path = "/discovery/search?q=a&save_history=false"

	discAssertStatus(t, discServe(t, r, http.MethodGet, path, nil), http.StatusOK)
	assertRateLimited(t, discServe(t, r, http.MethodGet, path, nil))

	now = now.Add(time.Minute)
	discAssertStatus(t, discServe(t, r, http.MethodGet, path, nil), http.StatusOK)
}
