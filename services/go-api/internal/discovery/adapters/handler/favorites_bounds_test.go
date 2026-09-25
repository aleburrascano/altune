package handler

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/discovery/service"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

func buildFavoritesLimitedRouter(limits DiscoveryRateLimits) chi.Router {
	h := NewDiscoveryHandler(DiscoveryServices{
		Favorites: service.NewFavoritesService(erroringFavoritesRepo{}),
	}).WithRateLimits(limits)
	r := chi.NewRouter()
	r.Use(auth.Middleware(discVerifyAsTestUser))
	r.Mount("/discovery", h.Routes())
	return r
}

func TestFavoritesRateLimit_WritesShareOneBudgetThenReject(t *testing.T) {
	router := buildFavoritesLimitedRouter(DiscoveryRateLimits{
		Favorites: RequestLimit{Max: 2, Window: time.Hour},
	})
	const body = `{"kind":"album","title":"DAMN.","subtitle":"Kendrick Lamar"}`

	discAssertStatus(t, discServe(t, router, http.MethodPut, "/discovery/favorites", strings.NewReader(body)), http.StatusOK)
	discAssertStatus(t, discServe(t, router, http.MethodDelete, "/discovery/favorites", strings.NewReader(body)), http.StatusNoContent)

	assertRateLimited(t, discServe(t, router, http.MethodPut, "/discovery/favorites", strings.NewReader(body)))
	assertRateLimited(t, discServe(t, router, http.MethodDelete, "/discovery/favorites", strings.NewReader(body)))
}

func TestFavoritesRateLimit_DefaultBudgetThenRejects(t *testing.T) {
	router := buildFavoritesLimitedRouter(DefaultDiscoveryRateLimits)
	const body = `{"kind":"album","title":"DAMN.","subtitle":"Kendrick Lamar"}`

	for i := range DefaultDiscoveryRateLimits.Favorites.Max {
		rec := discServe(t, router, http.MethodPut, "/discovery/favorites", strings.NewReader(body))
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d of %d = %d inside the budget", i+1, DefaultDiscoveryRateLimits.Favorites.Max, rec.Code)
		}
	}

	assertRateLimited(t, discServe(t, router, http.MethodPut, "/discovery/favorites", strings.NewReader(body)))
}

func TestFavoritesEndpoints_InvalidFavoriteIsATyped400(t *testing.T) {
	router := buildFavoritesLimitedRouter(DefaultDiscoveryRateLimits)
	bodies := map[string]string{
		"empty normalized key": `{"kind":"artist","title":"!!!"}`,
		"oversize title":       `{"kind":"album","title":"` + strings.Repeat("a", 201) + `"}`,
		"non-https image":      `{"kind":"album","title":"DAMN.","image_url":"http://img.example/a.jpg"}`,
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			rec := discServe(t, router, http.MethodPut, "/discovery/favorites", strings.NewReader(body))
			assertErrorCode(t, rec, http.StatusBadRequest, "discovery.invalid_favorite")
		})
	}
}
