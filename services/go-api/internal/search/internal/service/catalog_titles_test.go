package service_test

import (
	"altune/go-api/internal/search/internal/adapters/memory"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/ports"
	"altune/go-api/internal/search/internal/service"
	"context"
	"errors"
	"slices"
	"testing"
)

func TestGetArtistContentService_CatalogTitles_CoversTopTracksAndAlbums(t *testing.T) {
	svc := memory.NewArtistContent(t, map[domain.ProviderName]ports.ArtistContentProvider{
		domain.ProviderDeezer: &memory.ArtistContentProvider{
			TopTracks: []domain.SearchResult{memory.NewResult(
				domain.ResultKindTrack,
				"Magnolia",
				memory.WithSources([]domain.ProviderLink{{Provider: domain.ProviderDeezer, ExternalID: "t1"}}),
			)},
			Albums: []domain.SearchResult{memory.NewResult(
				domain.ResultKindAlbum,
				"Whole Lotta Red",
				memory.WithSources([]domain.ProviderLink{{Provider: domain.ProviderDeezer, ExternalID: "a1"}}),
			)},
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
	*memory.ArtistContentProvider
	tracklists map[string][]domain.SearchResult
	failing    map[string]bool
}

func (p *tracklistProvider) GetAlbumTracks(_ context.Context, _ domain.ProviderName, albumID string) ([]domain.SearchResult, error) {
	if p.failing[albumID] {
		return nil, errors.New("deezer down")
	}
	return p.tracklists[albumID], nil
}

func tracklistService(t testing.TB, failing map[string]bool) *service.GetArtistContentService {
	provider := newTracklistProvider(failing)
	return memory.NewArtistContent(t,
		map[domain.ProviderName]ports.ArtistContentProvider{domain.ProviderDeezer: provider},
		memory.ArtistTracklists(map[domain.ProviderName]ports.AlbumContentProvider{domain.ProviderDeezer: provider}))
}

func newTracklistProvider(failing map[string]bool) *tracklistProvider {
	return &tracklistProvider{
		ArtistContentProvider: &memory.ArtistContentProvider{
			Albums: []domain.SearchResult{
				memory.NewResult(
					domain.ResultKindAlbum,
					"Heaven or Hell",
					memory.WithSources([]domain.ProviderLink{{Provider: domain.ProviderDeezer, ExternalID: "a1"}}),
				),
				memory.NewResult(
					domain.ResultKindAlbum,
					"Life of a Don",
					memory.WithSources([]domain.ProviderLink{{Provider: domain.ProviderDeezer, ExternalID: "a2"}}),
				),
			},
		},
		tracklists: map[string][]domain.SearchResult{
			"a1": {memory.NewResult(domain.ResultKindTrack, "Deep Cut One")},
			"a2": {memory.NewResult(domain.ResultKindTrack, "Deep Cut Two")},
		},
		failing: failing,
	}
}

func TestGetArtistContentService_CatalogTitles_IncludesEveryAlbumTracklist(t *testing.T) {
	titles, err := tracklistService(t, nil).CatalogTitles(context.Background(), domain.ProviderDeezer, "artist-1", nil)
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
	titles, err := tracklistService(t, map[string]bool{"a1": true}).CatalogTitles(context.Background(), domain.ProviderDeezer, "artist-1", nil)
	if !errors.Is(err, domain.ErrPartialCatalog) {
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

func (p *panickingTracklistProvider) GetAlbumTracks(ctx context.Context, provider domain.ProviderName, albumID string) ([]domain.SearchResult, error) {
	if albumID == p.panicsOn {
		panic("tracklist parser exploded")
	}
	return p.tracklistProvider.GetAlbumTracks(ctx, provider, albumID)
}

func TestGetArtistContentService_CatalogTitles_PanickingTracklistIsPartialNotACrash(t *testing.T) {
	provider := &panickingTracklistProvider{tracklistProvider: newTracklistProvider(nil), panicsOn: "a1"}
	svc := memory.NewArtistContent(t,
		map[domain.ProviderName]ports.ArtistContentProvider{domain.ProviderDeezer: provider},
		memory.ArtistTracklists(map[domain.ProviderName]ports.AlbumContentProvider{domain.ProviderDeezer: provider}))

	titles, err := svc.CatalogTitles(context.Background(), domain.ProviderDeezer, "artist-1", nil)

	if !errors.Is(err, domain.ErrPartialCatalog) {
		t.Fatalf("err = %v, want ErrPartialCatalog", err)
	}
	if !slices.Contains(titles, "Deep Cut Two") || slices.Contains(titles, "Deep Cut One") {
		t.Errorf("titles = %v, want the second tracklist only", titles)
	}
}
