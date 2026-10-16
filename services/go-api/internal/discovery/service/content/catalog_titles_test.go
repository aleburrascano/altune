package content

import (
	"altune/go-api/internal/discovery/discoverytest"
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"context"
	"slices"
	"testing"
)

func TestGetArtistContentService_CatalogTitles_CoversTopTracksAndAlbums(t *testing.T) {
	svc := NewGetArtistContentService(map[domain.ProviderName]ports.ArtistContentProvider{
		domain.ProviderDeezer: &discoverytest.ArtistContentProvider{
			TopTracks: []domain.SearchResult{{Kind: domain.ResultKindTrack, Title: "Magnolia", Sources: []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "t1"}}}},
			Albums:    []domain.SearchResult{{Kind: domain.ResultKindAlbum, Title: "Whole Lotta Red", Sources: []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "a1"}}}},
		},
	})

	titles, err := svc.CatalogTitles(context.Background(), domain.ProviderDeezer, "artist-1", nil)
	if err != nil {
		t.Fatalf("CatalogTitles: %v", err)
	}

	for _, want := range []string{"Magnolia", "Whole Lotta Red"} {
		if !slices.Contains(titles, want) {
			t.Errorf("titles = %v, missing %q", titles, want)
		}
	}
}
