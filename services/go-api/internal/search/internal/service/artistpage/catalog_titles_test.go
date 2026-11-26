package artistpage_test

import (
	"altune/go-api/internal/search"
	"altune/go-api/internal/search/internal/searchtest"
	"context"
	"errors"
	"slices"
	"testing"
)

func TestGetArtistContentService_CatalogTitles_CoversTopTracksAndAlbums(t *testing.T) {
	svc := searchtest.NewArtistContent(t, map[search.ProviderName]search.ArtistContentProvider{
		search.ProviderDeezer: &search.FakeArtistContentProvider{
			TopTracks: []search.SearchResult{{Kind: search.ResultKindTrack, Title: "Magnolia", Sources: []search.ProviderLink{{Provider: search.ProviderDeezer, ExternalID: "t1"}}}},
			Albums:    []search.SearchResult{{Kind: search.ResultKindAlbum, Title: "Whole Lotta Red", Sources: []search.ProviderLink{{Provider: search.ProviderDeezer, ExternalID: "a1"}}}},
		},
	})

	titles, err := svc.CatalogTitles(context.Background(), search.ProviderDeezer, "artist-1", nil)
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
	*search.FakeArtistContentProvider
	tracklists map[string][]search.SearchResult
	failing    map[string]bool
}

func (p *tracklistProvider) GetAlbumTracks(_ context.Context, _ search.ProviderName, albumID string) ([]search.SearchResult, error) {
	if p.failing[albumID] {
		return nil, errors.New("deezer down")
	}
	return p.tracklists[albumID], nil
}

func tracklistService(t testing.TB, failing map[string]bool) *search.GetArtistContentService {
	return searchtest.NewArtistContent(t, map[search.ProviderName]search.ArtistContentProvider{search.ProviderDeezer: newTracklistProvider(failing)})
}

func newTracklistProvider(failing map[string]bool) *tracklistProvider {
	return &tracklistProvider{
		FakeArtistContentProvider: &search.FakeArtistContentProvider{
			Albums: []search.SearchResult{
				{Kind: search.ResultKindAlbum, Title: "Heaven or Hell", Sources: []search.ProviderLink{{Provider: search.ProviderDeezer, ExternalID: "a1"}}},
				{Kind: search.ResultKindAlbum, Title: "Life of a Don", Sources: []search.ProviderLink{{Provider: search.ProviderDeezer, ExternalID: "a2"}}},
			},
		},
		tracklists: map[string][]search.SearchResult{
			"a1": {{Kind: search.ResultKindTrack, Title: "Deep Cut One"}},
			"a2": {{Kind: search.ResultKindTrack, Title: "Deep Cut Two"}},
		},
		failing: failing,
	}
}

func TestGetArtistContentService_CatalogTitles_IncludesEveryAlbumTracklist(t *testing.T) {
	titles, err := tracklistService(t, nil).CatalogTitles(context.Background(), search.ProviderDeezer, "artist-1", nil)
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
	titles, err := tracklistService(t, map[string]bool{"a1": true}).CatalogTitles(context.Background(), search.ProviderDeezer, "artist-1", nil)
	if !errors.Is(err, search.ErrPartialCatalog) {
		t.Fatalf("err = %v, want ErrPartialCatalog", err)
	}
	if !slices.Contains(titles, "Deep Cut Two") || slices.Contains(titles, "Deep Cut One") {
		t.Errorf("titles = %v, want the second tracklist only", titles)
	}
}

type panickingTracklistProvider struct {
	*tracklistProvider
	panicsOn string
}

func (p *panickingTracklistProvider) GetAlbumTracks(ctx context.Context, provider search.ProviderName, albumID string) ([]search.SearchResult, error) {
	if albumID == p.panicsOn {
		panic("tracklist parser exploded")
	}
	return p.tracklistProvider.GetAlbumTracks(ctx, provider, albumID)
}

func TestGetArtistContentService_CatalogTitles_PanickingTracklistIsPartialNotACrash(t *testing.T) {
	provider := &panickingTracklistProvider{tracklistProvider: newTracklistProvider(nil), panicsOn: "a1"}
	svc := searchtest.NewArtistContent(t, map[search.ProviderName]search.ArtistContentProvider{search.ProviderDeezer: provider})

	titles, err := svc.CatalogTitles(context.Background(), search.ProviderDeezer, "artist-1", nil)

	if !errors.Is(err, search.ErrPartialCatalog) {
		t.Fatalf("err = %v, want ErrPartialCatalog", err)
	}
	if !slices.Contains(titles, "Deep Cut Two") || slices.Contains(titles, "Deep Cut One") {
		t.Errorf("titles = %v, want the second tracklist only", titles)
	}
}
