package handler_test

import (
	"altune/go-api/internal/catalog"
	shared "altune/go-api/internal/shared/kernel"
	"altune/go-api/internal/shared/requestuser"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func asBearer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
			if id, err := uuid.Parse(token); err == nil {
				r = r.WithContext(requestuser.ContextWithUserID(r.Context(), shared.NewUserId(id)))
			}
		}
		next.ServeHTTP(w, r)
	})
}

func trackRoutes(repo *catalog.InMemoryTrackRepo, opts ...func(*catalog.TrackHandler)) chi.Router {
	return trackRoutesWithBackfill(repo, catalog.NewBackfillFeaturedService(repo, repo, catalog.InMemoryFeaturedResolver{}), opts...)
}

func trackRoutesWithBackfill(repo *catalog.InMemoryTrackRepo, backfill *catalog.BackfillFeaturedService, opts ...func(*catalog.TrackHandler)) chi.Router {
	tracks := catalog.NewTrackHandler(
		catalog.NewAddTrackService(repo),
		catalog.NewListTracksService(repo),
		catalog.NewGetTrackStatusService(repo),
		catalog.NewDeleteTrackService(repo, catalog.NewInMemoryAudioStore()),
		catalog.NewSetTrackNumberService(repo),
		catalog.NewFeaturedArtistHandler(backfill, catalog.NewListFeaturingService(repo)),
		opts...,
	)
	router := chi.NewRouter()
	router.Use(asBearer)
	router.Route("/tracks", func(r chi.Router) { r.Mount("/", tracks.Routes()) })
	return router
}

func sendAs(router http.Handler, req *http.Request, user shared.UserId) *httptest.ResponseRecorder {
	req.Header.Set("Authorization", "Bearer "+user.String())
	return sendAnonymous(router, req)
}

func sendAnonymous(router http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func serve(router http.Handler, method, path string) *httptest.ResponseRecorder {
	return sendAs(router, httptest.NewRequest(method, path, nil), catalog.FixtureUserId())
}

func jsonRequest(t *testing.T, method, path string, v any) *http.Request {
	t.Helper()
	body, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("encode request body: %v", err)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func postJSON(t *testing.T, router http.Handler, path string, v any) *httptest.ResponseRecorder {
	t.Helper()
	return sendAs(router, jsonRequest(t, http.MethodPost, path, v), catalog.FixtureUserId())
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

func assertPrivateAudioHeaders(t *testing.T, rec *httptest.ResponseRecorder, wantCacheControl string) {
	t.Helper()
	if got := rec.Header().Get("Cache-Control"); got != wantCacheControl {
		t.Errorf("Cache-Control = %q, want %q", got, wantCacheControl)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
}
