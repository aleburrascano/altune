package handler

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/discovery/service"
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	discdomain "altune/go-api/internal/discovery/domain"

	"github.com/go-chi/chi/v5"
)

// healthContentProvider serves every content port, failing each call when down.
type healthContentProvider struct {
	provider discdomain.ProviderName
	down     bool
}

func (p healthContentProvider) answer(kind discdomain.ResultKind, id string) ([]discdomain.SearchResult, error) {
	if p.down {
		return nil, errors.New("upstream unavailable")
	}
	return []discdomain.SearchResult{{
		Kind:     kind,
		Title:    "Real Song",
		Subtitle: "Artist",
		Sources:  []discdomain.SourceRef{{Provider: p.provider, ExternalID: id}},
		Extras:   map[string]any{},
	}}, nil
}

func (p healthContentProvider) GetAlbumTracks(_ context.Context, _ discdomain.ProviderName, id string) ([]discdomain.SearchResult, error) {
	return p.answer(discdomain.ResultKindTrack, id)
}

func (p healthContentProvider) GetArtistTopTracks(_ context.Context, _ discdomain.ProviderName, id string) ([]discdomain.SearchResult, error) {
	return p.answer(discdomain.ResultKindTrack, id)
}

func (p healthContentProvider) GetArtistAlbums(_ context.Context, _ discdomain.ProviderName, id string) ([]discdomain.SearchResult, error) {
	return p.answer(discdomain.ResultKindAlbum, id)
}

func (p healthContentProvider) GetRelatedTracks(_ context.Context, _ discdomain.ProviderName, id string) ([]discdomain.SearchResult, error) {
	return p.answer(discdomain.ResultKindTrack, id)
}

// providerHealthContentRouter serves the discovery routes over real content
// services whose only provider is itunes, so a request naming any other
// provider finds no adapter wired for it.
func providerHealthContentRouter(itunesDown bool) (chi.Router, *fakeProviderHealth) {
	itunes := healthContentProvider{provider: discdomain.ProviderITunes, down: itunesDown}
	h := NewDiscoveryHandler(DiscoveryServices{
		Album: service.NewGetAlbumTracksService(
			map[discdomain.ProviderName]ports.AlbumContentProvider{discdomain.ProviderITunes: itunes}),
		Artist: service.NewGetArtistContentService(
			map[discdomain.ProviderName]ports.ArtistContentProvider{discdomain.ProviderITunes: itunes}),
		Related: service.NewGetRelatedTracksService(
			map[string]ports.RelatedTracksProvider{discdomain.ProviderITunes.String(): itunes}),
	})
	health := &fakeProviderHealth{}
	h.WithProviderHealth(health)
	router := chi.NewRouter()
	router.Use(auth.Middleware(discVerifyAsTestUser))
	router.Mount("/discovery", h.Routes())
	return router, health
}

var contentFetchHealthCases = []struct {
	name    string
	path    string
	samples int
}{
	{name: "album tracks", path: "/discovery/albums/itunes/id-1/tracks", samples: 1},
	{name: "artist top tracks", path: "/discovery/artists/itunes/id-1/top-tracks?name=Artist", samples: 1},
	{name: "artist albums", path: "/discovery/artists/itunes/id-1/albums?name=Artist", samples: 1},
	{name: "artist content", path: "/discovery/artists/itunes/id-1/content?name=Artist", samples: 2},
	{name: "related tracks", path: "/discovery/tracks/itunes/id-1/related", samples: 1},
}

func statusesRecordedFor(health *fakeProviderHealth, provider string) []string {
	statuses := make([]string, 0, len(health.records))
	for _, record := range health.records {
		recorded, status, isPair := strings.Cut(record, "/")
		if isPair && recorded == provider {
			statuses = append(statuses, status)
		}
	}
	return statuses
}

func TestContentFetchEndpoints_RecordProviderHealth(t *testing.T) {
	for _, down := range []bool{true, false} {
		wantStatus, wantHTTP := "ok", http.StatusOK
		if down {
			wantStatus, wantHTTP = "error", http.StatusBadGateway
		}
		for _, tc := range contentFetchHealthCases {
			t.Run(tc.name+"/"+wantStatus, func(t *testing.T) {
				router, health := providerHealthContentRouter(down)

				rec := discServe(t, router, http.MethodGet, tc.path, nil)
				discAssertStatus(t, rec, wantHTTP)

				statuses := statusesRecordedFor(health, "itunes")
				if len(statuses) != tc.samples {
					t.Fatalf("itunes health samples = %d (all records %v), want %d", len(statuses), health.records, tc.samples)
				}
				for _, status := range statuses {
					if status != wantStatus {
						t.Errorf("itunes health sample = %q, want %q", status, wantStatus)
					}
				}
			})
		}
	}
}

// A provider with no adapter wired for the content kind was never called, so
// the request says nothing about its health and must not mark it degraded.
func TestContentFetchEndpoints_UnservedProviderLeavesHealthUntouched(t *testing.T) {
	router, health := providerHealthContentRouter(false)
	for _, tc := range contentFetchHealthCases {
		t.Run(tc.name, func(t *testing.T) {
			rec := discServe(t, router, http.MethodGet, strings.Replace(tc.path, "/itunes/", "/spotify/", 1), nil)
			discAssertStatus(t, rec, http.StatusNotFound)
		})
	}
	if len(health.records) != 0 {
		t.Errorf("unserved provider requests recorded health samples: %v", health.records)
	}
}
