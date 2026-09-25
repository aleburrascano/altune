package handler

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/discovery/service"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	discdomain "altune/go-api/internal/discovery/domain"

	"github.com/go-chi/chi/v5"
)

type erroringFavoritesRepo struct{ err error }

func (r erroringFavoritesRepo) Add(context.Context, shared.UserId, discdomain.Favorite) error {
	return r.err
}

func (r erroringFavoritesRepo) Remove(context.Context, shared.UserId, discdomain.ResultKind, string) error {
	return r.err
}

func (r erroringFavoritesRepo) ListForUser(context.Context, shared.UserId) ([]discdomain.Favorite, error) {
	return nil, r.err
}

func buildFavoritesRouter(err error) chi.Router {
	svc := service.NewFavoritesService(erroringFavoritesRepo{err: err})
	h := NewDiscoveryHandler(DiscoveryServices{Favorites: svc})
	r := chi.NewRouter()
	r.Use(auth.Middleware(discVerifyAsTestUser))
	r.Mount("/discovery", h.Routes())
	return r
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
			name:       "validation",
			err:        shared.NewValidationError("discovery", "favorite is invalid"),
			wantStatus: http.StatusBadRequest,
			wantCode:   "discovery.validation_error",
		},
		{
			name:       "not found",
			err:        statusCodedError{status: http.StatusNotFound},
			wantStatus: http.StatusNotFound,
			wantCode:   "not_found",
		},
		{
			name:       "transient",
			err:        statusCodedError{status: http.StatusServiceUnavailable, code: "favorites_unavailable"},
			wantStatus: http.StatusServiceUnavailable,
			wantCode:   "favorites_unavailable",
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

func TestFavoritesEndpoints_OversizedBodyIsRejectedBeforeDecoding(t *testing.T) {
	router := buildFavoritesLimitedRouter(DefaultDiscoveryRateLimits)
	body := `{"kind":"album","title":"DAMN.","subtitle":"` + strings.Repeat("a", maxFavoriteBodyBytes) + `"}`
	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		rec := discServe(t, router, method, "/discovery/favorites", strings.NewReader(body))
		assertErrorCode(t, rec, http.StatusBadRequest, "discovery.invalid_body")
	}
}
