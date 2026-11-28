package service_test

import (
	"altune/go-api/internal/search"
	"altune/go-api/internal/search/internal/adapters/memory"
	"altune/go-api/internal/search/internal/ports"
	"altune/go-api/internal/search/internal/service"
	"altune/go-api/internal/search/searchtest"
	"context"
	"errors"
	"slices"
	"testing"
)

func TestGetArtistContentService_CatalogTitles_CoversTopTracksAndAlbums(t *testing.T) {
	svc := searchtest.NewArtistContent(t, map[search.ProviderName]ports.ArtistContentProvider{
		search.ProviderDeezer: &memory.ArtistContentProvider{
			TopTracks: []search.SearchResult{memory.NewResult(
				search.ResultKindTrack,
				"Magnolia",
				memory.WithSources([]search.ProviderLink{{Provider: search.ProviderDeezer, ExternalID: "t1"}}),
			)},
			Albums: []search.SearchResult{memory.NewResult(
				search.ResultKindAlbum,
				"Whole Lotta Red",
				memory.WithSources([]search.ProviderLink{{Provider: search.ProviderDeezer, ExternalID: "a1"}}),
			)},
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
	*memory.ArtistContentProvider
	tracklists map[string][]search.SearchResult
	failing    map[string]bool
}

func (p *tracklistProvider) GetAlbumTracks(_ context.Context, _ search.ProviderName, albumID string) ([]search.SearchResult, error) {
	if p.failing[albumID] {
		return nil, errors.New("deezer down")
	}
	return p.tracklists[albumID], nil
}

func tracklistService(t testing.TB, failing map[string]bool) *service.GetArtistContentService {
	provider := newTracklistProvider(failing)
	return searchtest.NewArtistContent(t,
		map[search.ProviderName]ports.ArtistContentProvider{search.ProviderDeezer: provider},
		searchtest.ArtistTracklists(map[search.ProviderName]ports.AlbumContentProvider{search.ProviderDeezer: provider}))
}

func newTracklistProvider(failing map[string]bool) *tracklistProvider {
	return &tracklistProvider{
		ArtistContentProvider: &memory.ArtistContentProvider{
			Albums: []search.SearchResult{
				memory.NewResult(
					search.ResultKindAlbum,
					"Heaven or Hell",
					memory.WithSources([]search.ProviderLink{{Provider: search.ProviderDeezer, ExternalID: "a1"}}),
				),
				memory.NewResult(
					search.ResultKindAlbum,
					"Life of a Don",
					memory.WithSources([]search.ProviderLink{{Provider: search.ProviderDeezer, ExternalID: "a2"}}),
				),
			},
		},
		tracklists: map[string][]search.SearchResult{
			"a1": {memory.NewResult(search.ResultKindTrack, "Deep Cut One")},
			"a2": {memory.NewResult(search.ResultKindTrack, "Deep Cut Two")},
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
	if !errors.Is(err, ports.ErrPartialCatalog) {
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
	svc := searchtest.NewArtistContent(t,
		map[search.ProviderName]ports.ArtistContentProvider{search.ProviderDeezer: provider},
		searchtest.ArtistTracklists(map[search.ProviderName]ports.AlbumContentProvider{search.ProviderDeezer: provider}))

	titles, err := svc.CatalogTitles(context.Background(), search.ProviderDeezer, "artist-1", nil)

	if !errors.Is(err, ports.ErrPartialCatalog) {
		t.Fatalf("err = %v, want ErrPartialCatalog", err)
	}
	if !slices.Contains(titles, "Deep Cut Two") || slices.Contains(titles, "Deep Cut One") {
		t.Errorf("titles = %v, want the second tracklist only", titles)
	}
}
