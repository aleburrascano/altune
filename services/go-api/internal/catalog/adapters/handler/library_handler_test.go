package handler

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/service"
	"net/http"
	"net/url"
	"strings"
	"testing"

	catdomain "altune/go-api/internal/catalog/domain"

	"github.com/go-chi/chi/v5"
)

func buildLibraryHandler(repo *catalogtest.TrackRepo) chi.Router {
	h := NewLibraryHandler(service.NewLibraryLensService(repo))
	r := chi.NewRouter()
	r.Use(auth.Middleware(verifyAsTestUser))
	r.Mount("/library", h.Routes())
	return r
}

func TestLibraryHandler_NegativeOffsetRejected(t *testing.T) {
	for _, path := range []string{"/library/albums", "/library/artists"} {
		t.Run(path, func(t *testing.T) {
			repo := catalogtest.NewTrackRepo()
			repo.Seed(makeTrack(testUserId, "One", "Metallica", "And Justice for All"))
			router := buildLibraryHandler(repo)

			rec := serve(t, router, http.MethodGet, path+"?offset=-1", nil)

			assertStatus(t, rec, http.StatusBadRequest)
		})
	}
}

func TestLibraryHandler_ZeroAndPositiveOffsetStillServed(t *testing.T) {
	for _, path := range []string{"/library/albums?offset=0", "/library/artists?offset=5"} {
		t.Run(path, func(t *testing.T) {
			repo := catalogtest.NewTrackRepo()
			repo.Seed(makeTrack(testUserId, "One", "Metallica", "And Justice for All"))
			router := buildLibraryHandler(repo)

			rec := serve(t, router, http.MethodGet, path, nil)

			assertStatus(t, rec, http.StatusOK)
		})
	}
}

func TestLibraryHandler_SearchTermLengthCapped(t *testing.T) {
	atCap := strings.Repeat("a", catdomain.MaxLibrarySearchLength)
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
			router := buildLibraryHandler(catalogtest.NewTrackRepo())

			rec := serve(t, router, http.MethodGet, tt.path+"?q="+url.QueryEscape(tt.q), nil)

			assertStatus(t, rec, tt.wantStatus)
		})
	}
}

// TestSearchTermWithNulByteRejected is the handler half of #2194: ?q=%00 used
// to reach an ILIKE against Postgres text and answer 500. Every list endpoint
// that accepts q shares libraryQuery, so all three are pinned here.
func TestSearchTermWithNulByteRejected(t *testing.T) {
	for _, path := range []string{"/library/albums", "/library/artists"} {
		t.Run(path, func(t *testing.T) {
			router := buildLibraryHandler(catalogtest.NewTrackRepo())

			rec := serve(t, router, http.MethodGet, path+"?q=%00", nil)

			assertStatus(t, rec, http.StatusBadRequest)
		})
	}
	t.Run("/tracks", func(t *testing.T) {
		_, router := buildTrackHandler(catalogtest.NewTrackRepo(), nil)

		rec := serve(t, router, http.MethodGet, "/tracks?q=%00", nil)

		assertStatus(t, rec, http.StatusBadRequest)
	})
}

func TestHandleListTracks_SearchTermLengthCapped(t *testing.T) {
	_, router := buildTrackHandler(catalogtest.NewTrackRepo(), nil)
	overCap := strings.Repeat("a", catdomain.MaxLibrarySearchLength+1)

	rec := serve(t, router, http.MethodGet, "/tracks?q="+overCap, nil)

	assertStatus(t, rec, http.StatusBadRequest)
}
