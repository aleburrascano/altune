package content

import (
	"altune/go-api/internal/discovery/discoverytest"
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"context"
	"errors"
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

type tracklistProvider struct {
	*discoverytest.ArtistContentProvider
	tracklists map[string][]domain.SearchResult
	failing    map[string]bool
}

func (p *tracklistProvider) GetAlbumTracks(_ context.Context, _ domain.ProviderName, albumID string) ([]domain.SearchResult, error) {
	if p.failing[albumID] {
		return nil, errors.New("deezer down")
	}
	return p.tracklists[albumID], nil
}

func tracklistService(failing map[string]bool) *GetArtistContentService {
	provider := &tracklistProvider{
		ArtistContentProvider: &discoverytest.ArtistContentProvider{
			Albums: []domain.SearchResult{
				{Kind: domain.ResultKindAlbum, Title: "Heaven or Hell", Sources: []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "a1"}}},
				{Kind: domain.ResultKindAlbum, Title: "Life of a Don", Sources: []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "a2"}}},
			},
		},
		tracklists: map[string][]domain.SearchResult{
			"a1": {{Kind: domain.ResultKindTrack, Title: "Deep Cut One"}},
			"a2": {{Kind: domain.ResultKindTrack, Title: "Deep Cut Two"}},
		},
		failing: failing,
	}
	return NewGetArtistContentService(map[domain.ProviderName]ports.ArtistContentProvider{domain.ProviderDeezer: provider})
}

func TestGetArtistContentService_CatalogTitles_IncludesEveryAlbumTracklist(t *testing.T) {
	titles, err := tracklistService(nil).CatalogTitles(context.Background(), domain.ProviderDeezer, "artist-1", nil)
	if err != nil {
		t.Fatalf("CatalogTitles: %v", err)
	}
	for _, want := range []string{"Deep Cut One", "Deep Cut Two", "Heaven or Hell"} {
		if !slices.Contains(titles, want) {
			t.Errorf("titles = %v, missing %q", titles, want)
		}
	}
}

func TestGetArtistContentService_CatalogTitles_FailedTracklistIsPartialButKeepsTheRest(t *testing.T) {
	titles, err := tracklistService(map[string]bool{"a1": true}).CatalogTitles(context.Background(), domain.ProviderDeezer, "artist-1", nil)
	if !errors.Is(err, ports.ErrPartialCatalog) {
		t.Fatalf("err = %v, want ErrPartialCatalog", err)
	}
	if !slices.Contains(titles, "Deep Cut Two") || slices.Contains(titles, "Deep Cut One") {
		t.Errorf("titles = %v, want the second tracklist only", titles)
	}
}
