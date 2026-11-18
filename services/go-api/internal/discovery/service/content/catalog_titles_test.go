package content_test

import (
	"altune/go-api/internal/discovery"
	"context"
	"errors"
	"slices"
	"testing"
)

func TestGetArtistContentService_CatalogTitles_CoversTopTracksAndAlbums(t *testing.T) {
	svc := discovery.NewGetArtistContentService(map[discovery.ProviderName]discovery.ArtistContentProvider{
		discovery.ProviderDeezer: &discovery.FakeArtistContentProvider{
			TopTracks: []discovery.SearchResult{{Kind: discovery.ResultKindTrack, Title: "Magnolia", Sources: []discovery.SourceRef{{Provider: discovery.ProviderDeezer, ExternalID: "t1"}}}},
			Albums:    []discovery.SearchResult{{Kind: discovery.ResultKindAlbum, Title: "Whole Lotta Red", Sources: []discovery.SourceRef{{Provider: discovery.ProviderDeezer, ExternalID: "a1"}}}},
		},
	})

	titles, err := svc.CatalogTitles(context.Background(), discovery.ProviderDeezer, "artist-1", nil)
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
	*discovery.FakeArtistContentProvider
	tracklists map[string][]discovery.SearchResult
	failing    map[string]bool
}

func (p *tracklistProvider) GetAlbumTracks(_ context.Context, _ discovery.ProviderName, albumID string) ([]discovery.SearchResult, error) {
	if p.failing[albumID] {
		return nil, errors.New("deezer down")
	}
	return p.tracklists[albumID], nil
}

func tracklistService(failing map[string]bool) *discovery.GetArtistContentService {
	provider := &tracklistProvider{
		FakeArtistContentProvider: &discovery.FakeArtistContentProvider{
			Albums: []discovery.SearchResult{
				{Kind: discovery.ResultKindAlbum, Title: "Heaven or Hell", Sources: []discovery.SourceRef{{Provider: discovery.ProviderDeezer, ExternalID: "a1"}}},
				{Kind: discovery.ResultKindAlbum, Title: "Life of a Don", Sources: []discovery.SourceRef{{Provider: discovery.ProviderDeezer, ExternalID: "a2"}}},
			},
		},
		tracklists: map[string][]discovery.SearchResult{
			"a1": {{Kind: discovery.ResultKindTrack, Title: "Deep Cut One"}},
			"a2": {{Kind: discovery.ResultKindTrack, Title: "Deep Cut Two"}},
		},
		failing: failing,
	}
	return discovery.NewGetArtistContentService(map[discovery.ProviderName]discovery.ArtistContentProvider{discovery.ProviderDeezer: provider})
}

func TestGetArtistContentService_CatalogTitles_IncludesEveryAlbumTracklist(t *testing.T) {
	titles, err := tracklistService(nil).CatalogTitles(context.Background(), discovery.ProviderDeezer, "artist-1", nil)
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
	titles, err := tracklistService(map[string]bool{"a1": true}).CatalogTitles(context.Background(), discovery.ProviderDeezer, "artist-1", nil)
	if !errors.Is(err, discovery.ErrPartialCatalog) {
		t.Fatalf("err = %v, want ErrPartialCatalog", err)
	}
	if !slices.Contains(titles, "Deep Cut Two") || slices.Contains(titles, "Deep Cut One") {
		t.Errorf("titles = %v, want the second tracklist only", titles)
	}
}
