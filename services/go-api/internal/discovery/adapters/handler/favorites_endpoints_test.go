package handler_test

import (
	"altune/go-api/internal/discovery"
	shared "altune/go-api/internal/shared/kernel"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	shareddomain "altune/go-api/internal/shared/domain"

	"github.com/go-chi/chi/v5"
)

type erroringFavoritesRepo struct{ err error }

func (r erroringFavoritesRepo) Add(context.Context, shared.UserId, discovery.Favorite) error {
	return r.err
}

func (r erroringFavoritesRepo) Remove(context.Context, shared.UserId, discovery.ResultKind, string) error {
	return r.err
}

func (r erroringFavoritesRepo) ListForUser(context.Context, shared.UserId) ([]discovery.Favorite, error) {
	return nil, r.err
}

func buildFavoritesRouter(err error) chi.Router {
	svc := discovery.NewFavoritesService(erroringFavoritesRepo{err: err})
	h := discovery.NewDiscoveryHandler(discovery.DiscoveryServices{Favorites: svc})
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
		{name: "a full list is a rejection, not an error", err: discovery.ErrFavoritesFull, wantStatus: http.StatusConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ring := captureLogs(t)

			rec := discServe(t, buildFavoritesRouter(tc.err), http.MethodPut, "/discovery/favorites", strings.NewReader(favBody))

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
			err:        fmt.Errorf("add favorite: %w", shareddomain.NewError(shareddomain.CodeDiscoveryInvalidFavorite, "favorite is invalid")),
			wantStatus: http.StatusBadRequest,
			wantCode:   "discovery.invalid_favorite",
		},
		{
			name:       "not found",
			err:        shareddomain.NewError(shareddomain.CodeDiscoveryContentUnserved, "favorite not served"),
			wantStatus: http.StatusNotFound,
			wantCode:   "discovery.content_unserved",
		},
		{
			name:       "unavailable",
			err:        shareddomain.NewError(shareddomain.CodeDiscoveryProviderCircuitOpen, "favorites paused"),
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
				router := buildFavoritesRouter(e.err)
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

func buildFavoritesLimitedRouter(limits discovery.DiscoveryRateLimits) chi.Router {
	h := discovery.NewDiscoveryHandler(discovery.DiscoveryServices{
		Favorites: discovery.NewFavoritesService(erroringFavoritesRepo{}),
	}).WithRateLimits(limits)
	r := chi.NewRouter()
	r.Use(discAsTestUser)
	r.Mount("/discovery", h.Routes())
	return r
}

func TestFavoritesRateLimit_WritesShareOneBudgetThenReject(t *testing.T) {
	router := buildFavoritesLimitedRouter(discovery.DiscoveryRateLimits{
		Favorites: discovery.RequestLimit{Max: 2, Window: time.Hour},
	})
	const body = `{"kind":"album","title":"DAMN.","subtitle":"Kendrick Lamar"}`

	discAssertStatus(t, discServe(t, router, http.MethodPut, "/discovery/favorites", strings.NewReader(body)), http.StatusOK)
	discAssertStatus(t, discServe(t, router, http.MethodDelete, "/discovery/favorites", strings.NewReader(body)), http.StatusNoContent)

	assertRateLimited(t, discServe(t, router, http.MethodPut, "/discovery/favorites", strings.NewReader(body)))
	assertRateLimited(t, discServe(t, router, http.MethodDelete, "/discovery/favorites", strings.NewReader(body)))
}

func TestFavoritesRateLimit_DefaultBudgetThenRejects(t *testing.T) {
	router := buildFrozenDefaultLimitsRouter(discovery.DiscoveryServices{
		Favorites: discovery.NewFavoritesService(erroringFavoritesRepo{}),
	})
	const body = `{"kind":"album","title":"DAMN.","subtitle":"Kendrick Lamar"}`

	for i := range discovery.DefaultDiscoveryRateLimits.Favorites.Max {
		rec := discServe(t, router, http.MethodPut, "/discovery/favorites", strings.NewReader(body))
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d of %d = %d inside the budget", i+1, discovery.DefaultDiscoveryRateLimits.Favorites.Max, rec.Code)
		}
	}

	assertRateLimited(t, discServe(t, router, http.MethodPut, "/discovery/favorites", strings.NewReader(body)))
}

func TestFavoritesEndpoints_InvalidFavoriteIsATyped400(t *testing.T) {
	router := buildFavoritesLimitedRouter(discovery.DefaultDiscoveryRateLimits)
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
	router := buildFavoritesLimitedRouter(discovery.DefaultDiscoveryRateLimits)
	body := `{"kind":"album","title":"DAMN.","subtitle":"` + strings.Repeat("a", 16384) + `"}`
	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		rec := discServe(t, router, method, "/discovery/favorites", strings.NewReader(body))
		assertErrorCode(t, rec, http.StatusRequestEntityTooLarge, "request.too_large")
	}
}

func TestFavoritesEndpoints_UnparseableBodyKeepsTheDiscoveryCode(t *testing.T) {
	router := buildFavoritesLimitedRouter(discovery.DefaultDiscoveryRateLimits)
	rec := discServe(t, router, http.MethodPut, "/discovery/favorites", strings.NewReader("{not json"))
	assertErrorCode(t, rec, http.StatusBadRequest, "discovery.invalid_body")
}
