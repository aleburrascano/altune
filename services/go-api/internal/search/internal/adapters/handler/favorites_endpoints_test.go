package handler_test

import (
	"altune/go-api/internal/search"
	"altune/go-api/internal/search/internal/adapters/handler"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/searchtest"
	"altune/go-api/internal/shared/ids"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	apierror "altune/go-api/internal/shared/apierror"

	"github.com/go-chi/chi/v5"
)

type erroringFavoritesRepo struct{ err error }

func (r erroringFavoritesRepo) Add(context.Context, ids.UserId, domain.Favorite) error {
	return r.err
}

func (r erroringFavoritesRepo) Remove(context.Context, ids.UserId, search.ResultKind, string) error {
	return r.err
}

func (r erroringFavoritesRepo) ListForUser(context.Context, ids.UserId) ([]domain.Favorite, error) {
	return nil, r.err
}

func buildFavoritesRouter(t *testing.T, err error) chi.Router {
	svc := searchtest.NewFavorites(t, erroringFavoritesRepo{err: err})
	h := handler.MustNewDiscoveryHandler(searchtest.CompleteServices(t, handler.DiscoveryServices{Favorites: svc}), time.Now)
	r := chi.NewRouter()
	r.Use(discAsTestUser)
	r.Mount("/discovery", h.Routes())
	return r
}

func TestFavoritesEndpoints_EachFailureIsLoggedOnceByTheBoundary(t *testing.T) {
	const favBody = `{"kind":"album","title":"DAMN.","subtitle":"Kendrick Lamar"}`
	for _, tc := range []struct {
		name       string
		err        error
		wantStatus int
		wantLoud   []string
	}{
		{name: "a store failure is one error record", err: errors.New("favorites store down"), wantStatus: http.StatusInternalServerError, wantLoud: []string{"request.failed"}},
		{name: "a full list is a rejection, not an error", err: domain.ErrFavoritesFull, wantStatus: http.StatusConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ring := captureLogs(t)

			rec := discServe(t, buildFavoritesRouter(t, tc.err), http.MethodPut, "/discovery/favorites", strings.NewReader(favBody))

			discAssertStatus(t, rec, tc.wantStatus)
			var loud []string
			for _, r := range ring.Snapshot() {
				if r.Level == "ERROR" || r.Level == "WARN" {
					loud = append(loud, r.Message)
				}
			}
			if !slices.Equal(loud, tc.wantLoud) {
				t.Errorf("warn and error records %v, want %v", loud, tc.wantLoud)
			}
		})
	}
}

func TestFavoritesEndpoints_ServiceErrorsUseTypedContract(t *testing.T) {
	const favBody = `{"kind":"album","title":"DAMN.","subtitle":"Kendrick Lamar"}`
	requests := []struct {
		name   string
		method string
		body   string
	}{
		{name: "list", method: http.MethodGet},
		{name: "add", method: http.MethodPut, body: favBody},
		{name: "remove", method: http.MethodDelete, body: favBody},
	}
	errs := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{
			name:       "invalid",
			err:        fmt.Errorf("add favorite: %w", apierror.NewError(apierror.CodeDiscoveryInvalidFavorite, "favorite is invalid")),
			wantStatus: http.StatusBadRequest,
			wantCode:   "discovery.invalid_favorite",
		},
		{
			name:       "conflict",
			err:        apierror.NewError(apierror.CodeDiscoveryFavoritesFull, "favorites are full"),
			wantStatus: http.StatusConflict,
			wantCode:   "discovery.favorites_full",
		},
		{
			name:       "unavailable",
			err:        apierror.NewError(apierror.CodeDiscoveryProviderCircuitOpen, "favorites paused"),
			wantStatus: http.StatusServiceUnavailable,
			wantCode:   "discovery.provider_circuit_open",
		},
		{
			name:       "unclassified",
			err:        errors.New("boom"),
			wantStatus: http.StatusInternalServerError,
			wantCode:   "internal",
		},
	}

	for _, req := range requests {
		for _, e := range errs {
			t.Run(req.name+" "+e.name, func(t *testing.T) {
				router := buildFavoritesRouter(t, e.err)
				var body io.Reader
				if req.body != "" {
					body = strings.NewReader(req.body)
				}
				rec := discServe(t, router, req.method, "/discovery/favorites", body)
				assertErrorCode(t, rec, e.wantStatus, e.wantCode)
			})
		}
	}
}

func buildFavoritesLimitedRouter(t *testing.T, limits handler.DiscoveryRateLimits) chi.Router {
	h := handler.MustNewDiscoveryHandler(searchtest.CompleteServices(t, handler.DiscoveryServices{
		Favorites: searchtest.NewFavorites(t, erroringFavoritesRepo{}),
	}), time.Now).WithRateLimits(limits)
	r := chi.NewRouter()
	r.Use(discAsTestUser)
	r.Mount("/discovery", h.Routes())
	return r
}

func TestFavoritesRateLimit_WritesShareOneBudgetThenReject(t *testing.T) {
	router := buildFavoritesLimitedRouter(t, handler.DiscoveryRateLimits{
		Favorites: handler.RequestLimit{Max: 2, Window: time.Hour},
	})
	const body = `{"kind":"album","title":"DAMN.","subtitle":"Kendrick Lamar"}`

	discAssertStatus(t, discServe(t, router, http.MethodPut, "/discovery/favorites", strings.NewReader(body)), http.StatusOK)
	discAssertStatus(t, discServe(t, router, http.MethodDelete, "/discovery/favorites", strings.NewReader(body)), http.StatusNoContent)

	assertRateLimited(t, discServe(t, router, http.MethodPut, "/discovery/favorites", strings.NewReader(body)))
	assertRateLimited(t, discServe(t, router, http.MethodDelete, "/discovery/favorites", strings.NewReader(body)))
}

func TestFavoritesRateLimit_DefaultBudgetThenRejects(t *testing.T) {
	router := buildFrozenDefaultLimitsRouter(searchtest.CompleteServices(t, handler.DiscoveryServices{
		Favorites: searchtest.NewFavorites(t, erroringFavoritesRepo{}),
	}))
	const body = `{"kind":"album","title":"DAMN.","subtitle":"Kendrick Lamar"}`

	for i := range handler.DefaultDiscoveryRateLimits.Favorites.Max {
		rec := discServe(t, router, http.MethodPut, "/discovery/favorites", strings.NewReader(body))
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d of %d = %d inside the budget", i+1, handler.DefaultDiscoveryRateLimits.Favorites.Max, rec.Code)
		}
	}

	assertRateLimited(t, discServe(t, router, http.MethodPut, "/discovery/favorites", strings.NewReader(body)))
}

func TestFavoritesEndpoints_InvalidFavoriteIsATyped400(t *testing.T) {
	router := buildFavoritesLimitedRouter(t, handler.DefaultDiscoveryRateLimits)
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

func TestFavoritesEndpoints_OversizedBodyIs413TooLarge(t *testing.T) {
	router := buildFavoritesLimitedRouter(t, handler.DefaultDiscoveryRateLimits)
	body := `{"kind":"album","title":"DAMN.","subtitle":"` + strings.Repeat("a", 16384) + `"}`
	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		rec := discServe(t, router, method, "/discovery/favorites", strings.NewReader(body))
		assertErrorCode(t, rec, http.StatusRequestEntityTooLarge, "request.too_large")
	}
}

func TestFavoritesEndpoints_UnparseableBodyKeepsTheDiscoveryCode(t *testing.T) {
	router := buildFavoritesLimitedRouter(t, handler.DefaultDiscoveryRateLimits)
	rec := discServe(t, router, http.MethodPut, "/discovery/favorites", strings.NewReader("{not json"))
	assertErrorCode(t, rec, http.StatusBadRequest, "discovery.invalid_body")
}
