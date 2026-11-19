package handler_test

import (
	"altune/go-api/internal/catalog"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func libraryRoutes(repo *catalog.InMemoryTrackRepo) chi.Router {
	library := catalog.NewLibraryHandler(catalog.NewLibraryLensService(repo))
	router := chi.NewRouter()
	router.Use(asBearer)
	router.Mount("/library", library.Routes())
	return router
}

func TestLibraryHandler_NegativeOffsetRejected(t *testing.T) {
	for _, path := range []string{"/library/albums", "/library/artists"} {
		t.Run(path, func(t *testing.T) {
			repo := catalog.NewInMemoryTrackRepo()
			catalog.SeedTrack(t, repo, catalog.FixtureUserId(), "One", "Metallica", "And Justice for All")
			router := libraryRoutes(repo)

			rec := serve(router, http.MethodGet, path+"?offset=-1")

			assertStatus(t, rec, http.StatusBadRequest)
		})
	}
}

func TestLibraryHandler_ZeroAndPositiveOffsetStillServed(t *testing.T) {
	for _, path := range []string{"/library/albums?offset=0", "/library/artists?offset=5"} {
		t.Run(path, func(t *testing.T) {
			repo := catalog.NewInMemoryTrackRepo()
			catalog.SeedTrack(t, repo, catalog.FixtureUserId(), "One", "Metallica", "And Justice for All")
			router := libraryRoutes(repo)

			rec := serve(router, http.MethodGet, path)

			assertStatus(t, rec, http.StatusOK)
		})
	}
}

func TestLibraryHandler_SearchTermLengthCapped(t *testing.T) {
	atCap := strings.Repeat("a", catalog.MaxLibrarySearchLength)
	overCap := atCap + "a"
	tests := []struct {
		name       string
		path       string
		q          string
		wantStatus int
	}{
		{"albums at cap", "/library/albums", atCap, http.StatusOK},
		{"albums over cap", "/library/albums", overCap, http.StatusBadRequest},
		{"artists over cap", "/library/artists", overCap, http.StatusBadRequest},
		{"surrounding whitespace not counted", "/library/artists", "  " + atCap + "  ", http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := libraryRoutes(catalog.NewInMemoryTrackRepo())

			rec := serve(router, http.MethodGet, tt.path+"?q="+url.QueryEscape(tt.q))

			assertStatus(t, rec, tt.wantStatus)
		})
	}
}

func TestSearchTermWithNulByteRejected(t *testing.T) {
	for _, path := range []string{"/library/albums", "/library/artists"} {
		t.Run(path, func(t *testing.T) {
			router := libraryRoutes(catalog.NewInMemoryTrackRepo())

			rec := serve(router, http.MethodGet, path+"?q=%00")

			assertStatus(t, rec, http.StatusBadRequest)
		})
	}
	t.Run("/tracks", func(t *testing.T) {
		router := trackRoutes(catalog.NewInMemoryTrackRepo())

		rec := serve(router, http.MethodGet, "/tracks?q=%00")

		assertStatus(t, rec, http.StatusBadRequest)
	})
}

func TestHandleListTracks_SearchTermLengthCapped(t *testing.T) {
	router := trackRoutes(catalog.NewInMemoryTrackRepo())
	overCap := strings.Repeat("a", catalog.MaxLibrarySearchLength+1)

	rec := serve(router, http.MethodGet, "/tracks?q="+overCap)

	assertStatus(t, rec, http.StatusBadRequest)
}
