package handler_test

import (
	"altune/go-api/internal/catalog"
	"altune/go-api/internal/shared/requestuser"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func asCaller(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(requestuser.ContextWithUserID(r.Context(), catalog.FixtureUserId())))
	})
}

func trackRoutes(repo *catalog.InMemoryTrackRepo) chi.Router {
	return trackRoutesWithBackfill(repo, catalog.NewBackfillFeaturedService(repo, repo, catalog.InMemoryFeaturedResolver{}))
}

func trackRoutesWithBackfill(repo *catalog.InMemoryTrackRepo, backfill *catalog.BackfillFeaturedService) chi.Router {
	tracks := catalog.NewTrackHandler(
		catalog.NewAddTrackService(repo),
		catalog.NewListTracksService(repo),
		catalog.NewGetTrackStatusService(repo),
		catalog.NewDeleteTrackService(repo, catalog.NewInMemoryAudioStore()),
		catalog.NewSetTrackNumberService(repo),
		catalog.NewFeaturedArtistHandler(backfill, catalog.NewListFeaturingService(repo)),
	)
	router := chi.NewRouter()
	router.Use(asCaller)
	router.Route("/tracks", func(r chi.Router) { r.Mount("/", tracks.Routes()) })
	return router
}

func serve(router http.Handler, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func assertStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Errorf("status = %d, want %d (body: %s)", rec.Code, want, rec.Body.String())
	}
}

func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder, dst any) {
	t.Helper()
	if err := json.NewDecoder(rec.Body).Decode(dst); err != nil {
		t.Fatalf("decodeJSON: %v (body: %s)", err, rec.Body.String())
	}
}

func assertErrorCode(t *testing.T, rec *httptest.ResponseRecorder, want string) {
	t.Helper()
	var body struct {
		Code string `json:"code"`
	}
	decodeJSON(t, rec, &body)
	if body.Code != want {
		t.Errorf("code = %q, want %q", body.Code, want)
	}
}
