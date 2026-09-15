package handler

import (
	"altune/go-api/internal/admin/providerhealth"
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
// services whose only provider is itunes, reporting into a real health store.
func providerHealthContentRouter(itunesDown bool) (chi.Router, *providerhealth.Store) {
	itunes := healthContentProvider{provider: discdomain.ProviderITunes, down: itunesDown}
	h := NewDiscoveryHandler(DiscoveryServices{
		Album: service.NewGetAlbumTracksService(
			map[discdomain.ProviderName]ports.AlbumContentProvider{discdomain.ProviderITunes: itunes}),
		Artist: service.NewGetArtistContentService(
			map[discdomain.ProviderName]ports.ArtistContentProvider{discdomain.ProviderITunes: itunes}),
		Related: service.NewGetRelatedTracksService(
			map[string]ports.RelatedTracksProvider{discdomain.ProviderITunes.String(): itunes}),
	})
	store := providerhealth.NewStore()
	h.WithProviderHealth(store)
	router := chi.NewRouter()
	router.Use(auth.Middleware(discVerifyAsTestUser))
	router.Mount("/discovery", h.Routes())
	return router, store
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

func snapshotFor(store *providerhealth.Store, provider string) (providerhealth.ProviderSnapshot, bool) {
	for _, snap := range store.Snapshot() {
		if snap.Provider == provider {
			return snap, true
		}
	}
	return providerhealth.ProviderSnapshot{}, false
}

func TestContentFetchEndpoints_RecordProviderHealth(t *testing.T) {
	for _, down := range []bool{true, false} {
		wantStatus, wantHTTP := "ok", http.StatusOK
		if down {
			wantStatus, wantHTTP = "error", http.StatusBadGateway
		}
		for _, tc := range contentFetchHealthCases {
			t.Run(tc.name+"/"+wantStatus, func(t *testing.T) {
				router, store := providerHealthContentRouter(down)

				rec := discServe(t, router, http.MethodGet, tc.path, nil)
				discAssertStatus(t, rec, wantHTTP)

				snap, ok := snapshotFor(store, "itunes")
				if !ok {
					t.Fatalf("provider-health snapshot has no itunes entry after a content fetch (got %+v)", store.Snapshot())
				}
				if snap.CurrentStatus != wantStatus {
					t.Errorf("itunes current status = %q, want %q", snap.CurrentStatus, wantStatus)
				}
				if snap.TotalCalls != tc.samples || snap.CountsPerStatus[wantStatus] != tc.samples {
					t.Errorf("itunes samples = %d (counts %v), want %d %q", snap.TotalCalls, snap.CountsPerStatus, tc.samples, wantStatus)
				}
				if down && snap.ErrorRate != 1 {
					t.Errorf("itunes error rate = %v, want 1", snap.ErrorRate)
				}
			})
		}
	}
}

// A provider with no adapter wired for the content kind was never called, so
// the request says nothing about its health and must not mark it degraded.
func TestContentFetchEndpoints_UnservedProviderLeavesHealthUntouched(t *testing.T) {
	router, store := providerHealthContentRouter(false)
	for _, tc := range contentFetchHealthCases {
		t.Run(tc.name, func(t *testing.T) {
			rec := discServe(t, router, http.MethodGet, strings.Replace(tc.path, "/itunes/", "/spotify/", 1), nil)
			discAssertStatus(t, rec, http.StatusNotFound)
		})
	}
	if snaps := store.Snapshot(); len(snaps) != 0 {
		t.Errorf("unserved provider requests recorded health samples: %+v", snaps)
	}
}
