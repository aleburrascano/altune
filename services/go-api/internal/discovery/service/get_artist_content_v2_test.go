package service

import (
	"context"
	"testing"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
)

func v2Album(provider domain.ProviderName, id, title string, opts ...func(*domain.SearchResult)) domain.SearchResult {
	r := domain.SearchResult{
		Kind:     domain.ResultKindAlbum,
		Title:    title,
		Subtitle: "Che",
		Sources:  []domain.SourceRef{{Provider: provider, ExternalID: id}},
		Extras:   map[string]any{},
	}
	for _, o := range opts {
		o(&r)
	}
	return r
}

// The end-to-end V2 discography: id-verified providers best-of-merge into complete
// albums (year + track-count + cover from whichever provider has each), while a
// same-name namesake arriving only via the by-name consensus feed is dropped.
func TestGetAlbums_v2_bestOfMergeAndNamesakeDropped(t *testing.T) {
	deezer := &fakeArtistContentProvider{
		getAlbumsFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
			return []domain.SearchResult{
				v2Album(domain.ProviderDeezer, "d-fl", "Fully Loaded",
					withDate("2026-04-01"), withCover("cover-dz"), withType("ep")),
			}, nil
		},
	}
	itunes := &fakeArtistContentProvider{
		getAlbumsFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
			// Corroborates Fully Loaded and carries the track count Deezer's list lacks.
			return []domain.SearchResult{
				v2Album(domain.ProviderITunes, "i-fl", "Fully Loaded", withTracks(5)),
			}, nil
		},
	}
	// A same-name artist's single, reachable only by name (no id, no MBID).
	namesake := consensusProvider("lastfm", "Wrong Che Single")
	consensus := NewConsensusService([]ConsensusProvider{namesake})

	store := &fakeIdentityStore{mbid: "mbid-che", xref: map[string]string{"deezer": "d1", "itunes": "i1"}}
	svc := NewGetArtistContentService(
		map[domain.ProviderName]ports.ArtistContentProvider{
			domain.ProviderDeezer: deezer,
			domain.ProviderITunes: itunes,
		},
		WithConsensusService(consensus),
		WithContentIdentityStore(store),
		WithIdentityFirst(),
		WithDiscographyV2(),
	)

	resp, err := svc.GetAlbums(context.Background(), domain.ProviderDeezer, "d1", "Che", 50)
	if err != nil {
		t.Fatalf("GetAlbums error = %v", err)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("items = %d, want 1 (Fully Loaded kept, namesake dropped): %+v", len(resp.Items), resp.Items)
	}
	a := resp.Items[0]
	if a.Title != "Fully Loaded" {
		t.Fatalf("title = %q, want Fully Loaded", a.Title)
	}
	if a.Year != 2026 {
		t.Errorf("Year = %d, want 2026 (normalized from Deezer's date)", a.Year)
	}
	if a.TrackCount != 5 {
		t.Errorf("TrackCount = %d, want 5 (best-of from iTunes)", a.TrackCount)
	}
	if a.ImageURL != "cover-dz" {
		t.Errorf("ImageURL = %q, want cover-dz (best-of from Deezer)", a.ImageURL)
	}
	if rt, _ := a.Extras["record_type"].(string); rt != "ep" {
		t.Errorf("record_type = %q, want ep", rt)
	}
	if len(a.Sources) != 2 {
		t.Errorf("sources = %d, want 2 (deezer + itunes unioned)", len(a.Sources))
	}
}

// The Che case: the durable store has NO cross-provider bridge (underground
// artist MusicBrainz doesn't url-relate), so resolveArtistIdentity returns
// ok=false. V2 must STILL run on the seed identity alone — the seed provider id
// is id-verified — keeping the seed's real albums and dropping a by-name namesake.
// Before this fix V2 was gated behind ok and silently fell back to the old path.
func TestGetAlbums_v2_runsOnSeedWhenStoreHasNoBridge(t *testing.T) {
	deezer := &fakeArtistContentProvider{
		getAlbumsFn: func(_ context.Context, _ domain.ProviderName, id string) ([]domain.SearchResult, error) {
			if id != "399574001" {
				t.Errorf("deezer queried id %q, want the seed 399574001", id)
			}
			return []domain.SearchResult{
				v2Album(domain.ProviderDeezer, "d-rib", "REST IN BASS: ENCORE", withDate("2025-12-25"), withCover("c")),
			}, nil
		},
	}
	namesake := consensusProvider("lastfm", "Wrong Che Album")
	svc := NewGetArtistContentService(
		map[domain.ProviderName]ports.ArtistContentProvider{domain.ProviderDeezer: deezer},
		WithConsensusService(NewConsensusService([]ConsensusProvider{namesake})),
		WithContentIdentityStore(&fakeIdentityStore{}), // empty → resolveArtistIdentity ok=false
		WithIdentityFirst(),
		WithDiscographyV2(),
	)

	resp, err := svc.GetAlbums(context.Background(), domain.ProviderDeezer, "399574001", "Che", 50)
	if err != nil {
		t.Fatalf("GetAlbums error = %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].Title != "REST IN BASS: ENCORE" {
		t.Fatalf("items = %+v, want just the id-verified ENCORE (namesake dropped, V2 ran on seed)", resp.Items)
	}
}

// V2 top-tracks: id-verified fan-out, corroborated tracks first, no by-name feed.
func TestGetTopTracks_v2_corroboratedFirst(t *testing.T) {
	deezer := &fakeArtistContentProvider{
		getTopTracksFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
			return []domain.SearchResult{
				trackFrom(domain.ProviderDeezer, "d-real", "Real Song", "Che"),
				trackFrom(domain.ProviderDeezer, "d-solo", "Solo", "Che"),
			}, nil
		},
	}
	itunes := &fakeArtistContentProvider{
		getTopTracksFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
			return []domain.SearchResult{trackFrom(domain.ProviderITunes, "i-real", "Real Song", "Che")}, nil
		},
	}
	store := &fakeIdentityStore{mbid: "mbid-che", xref: map[string]string{"deezer": "d1", "itunes": "i1"}}
	svc := NewGetArtistContentService(
		map[domain.ProviderName]ports.ArtistContentProvider{
			domain.ProviderDeezer: deezer,
			domain.ProviderITunes: itunes,
		},
		WithContentIdentityStore(store),
		WithIdentityFirst(),
		WithDiscographyV2(),
	)

	resp, err := svc.GetTopTracks(context.Background(), domain.ProviderDeezer, "d1", "Che", 10)
	if err != nil {
		t.Fatalf("GetTopTracks error = %v", err)
	}
	if len(resp.Items) != 2 {
		t.Fatalf("items = %d, want 2 merged", len(resp.Items))
	}
	if resp.Items[0].Title != "Real Song" {
		t.Errorf("first = %q, want Real Song (2-provider corroboration first)", resp.Items[0].Title)
	}
}
