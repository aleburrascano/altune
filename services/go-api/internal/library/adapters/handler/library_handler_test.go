package handler_test

import (
	"altune/go-api/internal/library"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func libraryRoutes(repo *library.InMemoryTrackRepo) chi.Router {
	library := library.NewLibraryHandler(library.NewLibraryLensService(repo))
	router := chi.NewRouter()
	router.Use(asBearer)
	router.Mount("/library", library.Routes())
	return router
}

func TestLibraryHandler_NegativeOffsetRejected(t *testing.T) {
	for _, path := range []string{"/library/albums", "/library/artists"} {
		t.Run(path, func(t *testing.T) {
			repo := library.NewInMemoryTrackRepo()
			library.SeedTrack(t, repo, library.FixtureUserId(), "One", "Metallica", "And Justice for All")
			router := libraryRoutes(repo)

			rec := request(t, router, http.MethodGet, path+"?offset=-1", nil)

			assertStatus(t, rec, http.StatusBadRequest)
		})
	}
}

func TestLibraryHandler_ZeroAndPositiveOffsetStillServed(t *testing.T) {
	for _, path := range []string{"/library/albums?offset=0", "/library/artists?offset=5"} {
		t.Run(path, func(t *testing.T) {
			repo := library.NewInMemoryTrackRepo()
			library.SeedTrack(t, repo, library.FixtureUserId(), "One", "Metallica", "And Justice for All")
			router := libraryRoutes(repo)

			rec := request(t, router, http.MethodGet, path, nil)

			assertStatus(t, rec, http.StatusOK)
		})
	}
}

func TestLibraryHandler_SearchTermLengthCapped(t *testing.T) {
	atCap := strings.Repeat("a", library.MaxLibrarySearchLength)
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
			router := libraryRoutes(library.NewInMemoryTrackRepo())

			rec := request(t, router, http.MethodGet, tt.path+"?q="+url.QueryEscape(tt.q), nil)

			assertStatus(t, rec, tt.wantStatus)
		})
	}
}

func TestSearchTermWithNulByteRejected(t *testing.T) {
	for _, path := range []string{"/library/albums", "/library/artists"} {
		t.Run(path, func(t *testing.T) {
			router := libraryRoutes(library.NewInMemoryTrackRepo())

			rec := request(t, router, http.MethodGet, path+"?q=%00", nil)

			assertStatus(t, rec, http.StatusBadRequest)
		})
	}
	t.Run("/tracks", func(t *testing.T) {
		router := trackRoutes(library.NewInMemoryTrackRepo())

		rec := request(t, router, http.MethodGet, "/tracks?q=%00", nil)

		assertStatus(t, rec, http.StatusBadRequest)
	})
}

func TestHandleListTracks_SearchTermLengthCapped(t *testing.T) {
	router := trackRoutes(library.NewInMemoryTrackRepo())
	overCap := strings.Repeat("a", library.MaxLibrarySearchLength+1)

	rec := request(t, router, http.MethodGet, "/tracks?q="+overCap, nil)

	assertStatus(t, rec, http.StatusBadRequest)
}
