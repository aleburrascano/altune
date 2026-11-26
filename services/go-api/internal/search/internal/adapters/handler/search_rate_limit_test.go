package handler_test

import (
	"altune/go-api/internal/search"
	"altune/go-api/internal/search/internal/adapters/handler"
	"altune/go-api/internal/search/internal/service/vocabulary"
	"altune/go-api/internal/search/searchtest"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/httpserver"
	"altune/go-api/internal/shared/ids"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func TestNewDiscoveryHandler_RefusesANilClock(t *testing.T) {
	h, err := handler.NewDiscoveryHandler(searchtest.CompleteServices(t, handler.DiscoveryServices{}), nil)

	if !errors.Is(err, handler.ErrClockRequired) || h != nil {
		t.Fatalf("NewDiscoveryHandler(nil clock) = (%v, %v), want handler.ErrClockRequired and no handler", h, err)
	}
}

func TestNewDiscoveryHandler_NamesEveryMissingService(t *testing.T) {
	h, err := handler.NewDiscoveryHandler(handler.DiscoveryServices{}, time.Now)

	if !errors.Is(err, handler.ErrServiceRequired) || h != nil {
		t.Fatalf("NewDiscoveryHandler(no services) = (%v, %v), want handler.ErrServiceRequired and no handler", h, err)
	}
	for _, name := range []string{"search", "album", "unreleased", "lyrics", "ownership", "client events"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error %q does not name %q", err, name)
		}
	}
}

func TestNewDiscoveryHandler_RefusesOneMissingService(t *testing.T) {
	services := searchtest.CompleteServices(t, handler.DiscoveryServices{})
	services.ClientEvents = nil

	_, err := handler.NewDiscoveryHandler(services, time.Now)

	if !errors.Is(err, handler.ErrServiceRequired) || !strings.Contains(err.Error(), "missing client events") {
		t.Fatalf("err = %v, want only client events named", err)
	}
}

func TestDiscoveryHandler_AttachOwnershipRefusesNil(t *testing.T) {
	h := handler.MustNewDiscoveryHandler(searchtest.CompleteServices(t, handler.DiscoveryServices{}), time.Now)

	if err := h.AttachOwnership(nil); !errors.Is(err, handler.ErrOwnershipRequired) {
		t.Fatalf("AttachOwnership(nil) = %v, want handler.ErrOwnershipRequired", err)
	}
}

func buildFrozenDefaultLimitsRouter(services handler.DiscoveryServices, middlewares ...func(http.Handler) http.Handler) chi.Router {
	frozen := time.Unix(1_700_000_000, 0)
	h := handler.MustNewDiscoveryHandler(services, func() time.Time { return frozen })
	r := chi.NewRouter()
	r.Use(discAsTestUser)
	r.Use(middlewares...)
	r.Mount("/discovery", h.Routes())
	return r
}

func deezerSearchServices(t *testing.T) handler.DiscoveryServices {
	bg := searchtest.NewTracker(t)
	providers := []search.SearchProvider{&fakeSearchProvider{name: search.ProviderDeezer}}
	return searchtest.CompleteServices(t, handler.DiscoveryServices{Search: searchtest.NewSearchService(t, bg, providers, search.NewCircuitBreaker(clock.Now))})
}

func TestSearchRateLimit_RapidSearchesAreThrottled(t *testing.T) {
	router := buildFrozenDefaultLimitsRouter(deezerSearchServices(t))

	throttled := 0
	for range 200 {
		rec := discServe(t, router, http.MethodGet, "/discovery/search?q=radiohead&save_history=false", nil)
		if rec.Code == http.StatusTooManyRequests {
			throttled++
		}
	}
	if want := 200 - handler.DefaultDiscoveryRateLimits.Search.Max; throttled != want {
		t.Fatalf("200 rapid /search requests from one user: %d throttled, want %d", throttled, want)
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
			router: buildFrozenDefaultLimitsRouter(deezerSearchServices(t)),
			path:   "/discovery/search?q=radiohead&save_history=false",
			max:    handler.DefaultDiscoveryRateLimits.Search.Max,
		},
		{
			name:   "suggest",
			router: buildFrozenDefaultLimitsRouter(searchtest.CompleteServices(t, handler.DiscoveryServices{Suggest: vocabulary.NewSuggestService(&fakeVocabStore{})})),
			path:   "/discovery/suggest?q=radio",
			max:    handler.DefaultDiscoveryRateLimits.Suggest.Max,
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
	router := buildFrozenDefaultLimitsRouter(searchtest.CompleteServices(t, handler.DiscoveryServices{}))

	var throttled *httptest.ResponseRecorder
	count := 0
	for range 200 {
		rec := discServe(t, router, http.MethodGet, "/discovery/lyrics?title=paranoid+android", nil)
		if rec.Code == http.StatusTooManyRequests {
			throttled = rec
			count++
		}
	}

	if want := 200 - handler.DefaultDiscoveryRateLimits.Content.Max; count != want {
		t.Fatalf("200 rapid /lyrics requests from one user: %d throttled, want %d", count, want)
	}
	assertRateLimited(t, throttled)
}

func TestContentRateLimit_OneBudgetSpansEveryContentRoute(t *testing.T) {
	router := buildRateLimitedRouter(t, handler.DiscoveryRateLimits{
		Search:  handler.RequestLimit{Max: 10, Window: time.Hour},
		Content: handler.RequestLimit{Max: 2, Window: time.Hour},
	})

	for range 2 {
		discAssertStatus(t, discServe(t, router, http.MethodGet, "/discovery/lyrics?title=a", nil), http.StatusOK)
	}

	assertRateLimited(t, discServe(t, router, http.MethodGet, "/discovery/enrichment?kind=track&title=a", nil))
	discAssertStatus(t, discServe(t, router, http.MethodGet, "/discovery/search?q=a&save_history=false", nil), http.StatusOK)
}

func TestEventRateLimit_DefaultBudgetThenRejects(t *testing.T) {
	router := buildFrozenDefaultLimitsRouter(
		searchtest.CompleteServices(t, handler.DiscoveryServices{Event: searchtest.NewRecordEvent(t, &recordingEventStore{})}),
		httpserver.MaxBodySize(1<<20),
	)
	body := discJsonBody(t, map[string]any{"type": "play"}).String()

	for i := range handler.DefaultDiscoveryRateLimits.Events.Max {
		rec := discServe(t, router, http.MethodPost, "/discovery/events", strings.NewReader(body))
		if rec.Code == http.StatusTooManyRequests {
			t.Fatalf("request %d of %d throttled inside the budget", i+1, handler.DefaultDiscoveryRateLimits.Events.Max)
		}
	}

	assertRateLimited(t, discServe(t, router, http.MethodPost, "/discovery/events", strings.NewReader(body)))
}

func TestSearchRateLimit_BudgetsArePerUserAndPerRoute(t *testing.T) {
	bg := searchtest.NewTracker(t)
	h := handler.MustNewDiscoveryHandler(searchtest.CompleteServices(t, handler.DiscoveryServices{
		Search:  searchtest.NewSearchService(t, bg, nil, search.NewCircuitBreaker(clock.Now)),
		Suggest: vocabulary.NewSuggestService(&fakeVocabStore{}),
	}), time.Now).WithRateLimits(handler.DiscoveryRateLimits{
		Search:  handler.RequestLimit{Max: 2, Window: time.Hour},
		Suggest: handler.RequestLimit{Max: 2, Window: time.Hour},
	})
	r := chi.NewRouter()
	r.Use(discAsTokenUser)
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
	h := handler.MustNewDiscoveryHandler(searchtest.CompleteServices(t, handler.DiscoveryServices{}), time.Now).WithRateLimits(handler.DiscoveryRateLimits{
		Search:  handler.RequestLimit{Max: 0, Window: time.Hour},
		Suggest: handler.RequestLimit{Max: 0, Window: time.Hour},
	})
	r := chi.NewRouter()
	r.Mount("/discovery", h.Routes())

	discAssertStatus(t, discServeNoAuth(t, r, http.MethodGet, "/discovery/search?q=a"), http.StatusUnauthorized)
}

func TestRequestLimit_BucketAdmitsBurstThenRefillsAtTheWindowRate(t *testing.T) {
	bg := searchtest.NewTracker(t)
	now := time.Unix(1_700_000_000, 0)
	h := handler.MustNewDiscoveryHandler(searchtest.CompleteServices(t, handler.DiscoveryServices{
		Search: searchtest.NewSearchService(t, bg, nil, search.NewCircuitBreaker(clock.Now)),
	}), func() time.Time { return now }).WithRateLimits(handler.DiscoveryRateLimits{Search: handler.RequestLimit{Max: 2, Window: time.Minute}})
	r := chi.NewRouter()
	r.Use(discAsTestUser)
	r.Mount("/discovery", h.Routes())
	serve := func() int {
		return discServe(t, r, http.MethodGet, "/discovery/search?q=a&save_history=false", nil).Code
	}

	for i := range 2 {
		if got := serve(); got != http.StatusOK {
			t.Fatalf("burst request %d = %d, want 200", i+1, got)
		}
	}
	if got := serve(); got != http.StatusTooManyRequests {
		t.Fatalf("request past the burst = %d, want 429", got)
	}
	now = now.Add(30 * time.Second)
	if got := serve(); got != http.StatusOK {
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
		if got := httpserver.RetryAfterSeconds(wait); got != want {
			t.Errorf("RetryAfterSeconds(%v) = %q, want %q", wait, got, want)
		}
	}
}

var otherTestUserId = ids.NewUserId(uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"))

var discAsTokenUser = signedInAs(func(token string) ids.UserId {
	if token == "other" {
		return otherTestUserId
	}
	return discTestUserId
})

func buildRateLimitedRouter(t *testing.T, limits handler.DiscoveryRateLimits) chi.Router {
	bg := searchtest.NewTracker(t)
	h := handler.MustNewDiscoveryHandler(searchtest.CompleteServices(t, handler.DiscoveryServices{
		Search: searchtest.NewSearchService(t, bg, nil, search.NewCircuitBreaker(clock.Now)),
	}), time.Now).WithRateLimits(limits)
	r := chi.NewRouter()
	r.Use(discAsTestUser)
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
	var resp httpserver.Problem
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
			h := handler.MustNewDiscoveryHandler(searchtest.CompleteServices(t, handler.DiscoveryServices{
				History:      searchtest.NewListHistory(t, historyRepo),
				ClearHistory: searchtest.NewClearHistory(t, historyRepo),
				Favorites:    searchtest.NewFavorites(t, erroringFavoritesRepo{}),
			}), time.Now).WithRateLimits(handler.DiscoveryRateLimits{
				History:   handler.RequestLimit{Max: 1, Window: time.Hour},
				Favorites: handler.RequestLimit{Max: 1, Window: time.Hour},
			})
			r := chi.NewRouter()
			r.Use(discAsTokenUser)
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
	bg := searchtest.NewTracker(t)
	now := time.Unix(1_700_000_000, 0)
	h := handler.MustNewDiscoveryHandler(searchtest.CompleteServices(t, handler.DiscoveryServices{
		Search: searchtest.NewSearchService(t, bg, nil, search.NewCircuitBreaker(clock.Now)),
	}), func() time.Time { return now }).WithRateLimits(handler.DiscoveryRateLimits{Search: handler.RequestLimit{Max: 1, Window: time.Minute}})
	r := chi.NewRouter()
	r.Use(discAsTestUser)
	r.Mount("/discovery", h.Routes())
	const path = "/discovery/search?q=a&save_history=false"

	discAssertStatus(t, discServe(t, r, http.MethodGet, path, nil), http.StatusOK)
	assertRateLimited(t, discServe(t, r, http.MethodGet, path, nil))

	now = now.Add(time.Minute)
	discAssertStatus(t, discServe(t, r, http.MethodGet, path, nil), http.StatusOK)
}
