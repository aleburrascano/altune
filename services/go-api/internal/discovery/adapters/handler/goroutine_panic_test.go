package handler

import (
	"altune/go-api/internal/discovery/ports"
	"context"
	"net/http"
	"testing"

	discdomain "altune/go-api/internal/discovery/domain"
)

// These tests guard issue #568: a panic inside a goroutine the handler spawns
// must be contained. Without recovery each one crashes the test binary.

type panickingArtistContentProvider struct{}

func (panickingArtistContentProvider) GetArtistTopTracks(context.Context, discdomain.ProviderName, string) ([]discdomain.SearchResult, error) {
	panic("top tracks exploded")
}

func (panickingArtistContentProvider) GetArtistAlbums(context.Context, discdomain.ProviderName, string) ([]discdomain.SearchResult, error) {
	panic("albums exploded")
}

func TestHandleArtistContent_PanickingProviderReturns500(t *testing.T) {
	artistProviders := map[discdomain.ProviderName]ports.ArtistContentProvider{
		discdomain.ProviderDeezer: panickingArtistContentProvider{},
	}
	router := buildDiscoveryRouter(nil, &fakeSearchHistoryRepo{}, nil, artistProviders)

	rec := discServe(t, router, http.MethodGet, "/discovery/artists/deezer/1/content", nil)
	discAssertStatus(t, rec, http.StatusInternalServerError)
}
