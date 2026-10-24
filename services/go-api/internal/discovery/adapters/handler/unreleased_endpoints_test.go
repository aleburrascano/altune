package handler

import (
	"altune/go-api/internal/auth/adapters/middleware"
	"altune/go-api/internal/discovery/discoverytest"
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/service/unreleased"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

func unreleasedRouter(genius *discoverytest.GeniusSongCatalog, catalog []string) chi.Router {
	svc := unreleased.NewService(genius, &discoverytest.ArtistCatalogTitles{Titles: catalog}, nil, unreleased.Config{MaxPages: 1, MaxDetails: 10})
	h := NewDiscoveryHandler(DiscoveryServices{}).WithUnreleased(svc)
	r := chi.NewRouter()
	r.Use(middleware.Middleware(discVerifyAsTestUser))
	r.Mount("/discovery", h.Routes())
	return r
}

func decodeUnreleased(t *testing.T, body []byte) UnreleasedResponseDTO {
	t.Helper()
	var dto UnreleasedResponseDTO
	if err := json.Unmarshal(body, &dto); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	return dto
}

func TestHandleArtistUnreleased_NoGeniusMatchIsAnEmptyList(t *testing.T) {
	router := unreleasedRouter(&discoverytest.GeniusSongCatalog{ArtistFound: false}, nil)

	rec := discServe(t, router, http.MethodGet, "/discovery/artists/deezer/999/unreleased?name=Nobody", nil)

	discAssertStatus(t, rec, http.StatusOK)
	if got := rec.Body.String(); got != "{\"items\":[],\"pending\":false}\n" {
		t.Errorf("body = %q, want an empty items list", got)
	}
}

func TestHandleArtistUnreleased_WithoutAnArtistNameIsAnEmptyList(t *testing.T) {
	router := unreleasedRouter(&discoverytest.GeniusSongCatalog{ArtistFound: true}, nil)

	rec := discServe(t, router, http.MethodGet, "/discovery/artists/deezer/1/unreleased", nil)

	discAssertStatus(t, rec, http.StatusOK)
	if items := decodeUnreleased(t, rec.Body.Bytes()).Items; len(items) != 0 {
		t.Errorf("items = %v, want none", items)
	}
}

func TestHandleArtistUnreleased_ServiceNotConfiguredIsAnEmptyList(t *testing.T) {
	h := NewDiscoveryHandler(DiscoveryServices{})
	r := chi.NewRouter()
	r.Use(middleware.Middleware(discVerifyAsTestUser))
	r.Mount("/discovery", h.Routes())

	rec := discServe(t, r, http.MethodGet, "/discovery/artists/deezer/1/unreleased?name=Don", nil)

	discAssertStatus(t, rec, http.StatusOK)
	if items := decodeUnreleased(t, rec.Body.Bytes()).Items; len(items) != 0 {
		t.Errorf("items = %v, want none", items)
	}
}

func TestHandleArtistUnreleased_ReturnsUnreleasedSongsWithTheAppShape(t *testing.T) {
	genius := &discoverytest.GeniusSongCatalog{
		ArtistID: 7, ArtistFound: true,
		Songs: []domain.UnreleasedSong{
			{GeniusID: 1, Title: "Magnolia", GeniusURL: "https://genius.com/m"},
			{GeniusID: 2, Title: "Cancun", GeniusURL: "https://genius.com/c", CoverURL: "https://img/c.jpg", ReleaseDate: "2022-07-24"},
			{GeniusID: 3, Title: "Naruto", GeniusURL: "https://genius.com/n"},
		},
		MediaByID: map[int]bool{2: true},
	}
	router := unreleasedRouter(genius, []string{"Magnolia"})

	rec := discServe(t, router, http.MethodGet, "/discovery/artists/deezer/1/unreleased?name=Playboi+Carti", nil)

	discAssertStatus(t, rec, http.StatusOK)
	items := decodeUnreleased(t, rec.Body.Bytes()).Items
	if len(items) != 2 || items[0].Title != "Cancun" || items[1].Title != "Naruto" {
		t.Fatalf("items = %+v, want Cancun then Naruto", items)
	}
	cancun, naruto := items[0], items[1]
	if cancun.CoverURL == nil || *cancun.CoverURL != "https://img/c.jpg" || cancun.ReleaseDate == nil || cancun.HasMedia == nil || !*cancun.HasMedia {
		t.Errorf("cancun = %+v, want cover, release date and has_media", cancun)
	}
	if naruto.CoverURL != nil || naruto.ReleaseDate != nil || (naruto.HasMedia != nil && *naruto.HasMedia) {
		t.Errorf("naruto = %+v, want null cover, null release date and no media", naruto)
	}
}

func TestHandleArtistUnreleased_UpstreamFailureIsAnError(t *testing.T) {
	router := unreleasedRouter(&discoverytest.GeniusSongCatalog{ArtistFound: true, Err: errors.New("genius unavailable")}, nil)

	rec := discServe(t, router, http.MethodGet, "/discovery/artists/deezer/1/unreleased?name=Don", nil)

	if rec.Code < 500 {
		t.Errorf("status = %d, want a 5xx", rec.Code)
	}
}

func TestHandleArtistUnreleased_SlowJobAnswersPendingWithNoItems(t *testing.T) {
	genius := &discoverytest.GeniusSongCatalog{ArtistFound: true, Songs: []domain.UnreleasedSong{{GeniusID: 1, Title: "Leak"}}, Gate: make(chan struct{})}
	svc := unreleased.NewService(genius, &discoverytest.ArtistCatalogTitles{}, nil, unreleased.Config{MaxPages: 1, MaxDetails: 10, Wait: 10 * time.Millisecond})
	h := NewDiscoveryHandler(DiscoveryServices{}).WithUnreleased(svc)
	r := chi.NewRouter()
	r.Use(middleware.Middleware(discVerifyAsTestUser))
	r.Mount("/discovery", h.Routes())

	rec := discServe(t, r, http.MethodGet, "/discovery/artists/deezer/1/unreleased?name=Artist", nil)

	discAssertStatus(t, rec, http.StatusOK)
	if got := rec.Body.String(); got != "{\"items\":[],\"pending\":true}\n" {
		t.Errorf("body = %q, want pending with no items", got)
	}
}

func TestHandleArtistUnreleased_ReturnsAudioLinksAsAlwaysAnArray(t *testing.T) {
	genius := &discoverytest.GeniusSongCatalog{
		ArtistID: 7, ArtistFound: true,
		Songs: []domain.UnreleasedSong{
			{GeniusID: 1, Title: "Cancun", GeniusURL: "https://genius.com/c"},
			{GeniusID: 2, Title: "Naruto", GeniusURL: "https://genius.com/n"},
		},
		DetailByID: map[int]domain.GeniusSongDetail{
			1: {HasMedia: true, AudioLinks: []domain.AudioLink{{Provider: "soundcloud", URL: "https://soundcloud.com/a/b"}}},
		},
	}
	router := unreleasedRouter(genius, nil)

	rec := discServe(t, router, http.MethodGet, "/discovery/artists/deezer/1/unreleased?name=Artist", nil)

	discAssertStatus(t, rec, http.StatusOK)
	items := decodeUnreleased(t, rec.Body.Bytes()).Items
	want := domain.AudioLink{Provider: "soundcloud", URL: "https://soundcloud.com/a/b"}
	if len(items) != 2 || len(items[0].AudioLinks) != 1 || items[0].AudioLinks[0] != want {
		t.Fatalf("items = %+v, want the soundcloud link on Cancun", items)
	}
	if !strings.Contains(rec.Body.String(), `"audio_links":[]`) {
		t.Errorf("body = %s, want an empty audio_links array for Naruto", rec.Body.String())
	}
}
