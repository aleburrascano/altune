package service

import (
	"context"
	"testing"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
)

func trackFrom(provider domain.ProviderName, id, title, artist string) domain.SearchResult {
	return domain.SearchResult{
		Kind:     domain.ResultKindTrack,
		Title:    title,
		Subtitle: artist,
		Sources:  []domain.SourceRef{{Provider: provider, ExternalID: id}},
	}
}

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
			return []domain.SearchResult{
				v2Album(domain.ProviderITunes, "i-fl", "Fully Loaded", withTracks(5)),
			}, nil
		},
	}
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
		WithContentIdentityStore(&fakeIdentityStore{}),
	)

	resp, err := svc.GetAlbums(context.Background(), domain.ProviderDeezer, "399574001", "Che", 50)
	if err != nil {
		t.Fatalf("GetAlbums error = %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].Title != "REST IN BASS: ENCORE" {
		t.Fatalf("items = %+v, want just the id-verified ENCORE (namesake dropped, V2 ran on seed)", resp.Items)
	}
}

func TestGetAlbums_v2_deterministicAcrossRuns(t *testing.T) {
	deezer := &fakeArtistContentProvider{
		getAlbumsFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
			return []domain.SearchResult{
				v2Album(domain.ProviderDeezer, "d-fl", "Fully Loaded", withDate("2026-04-01")),
			}, nil
		},
	}
	itunes := &fakeArtistContentProvider{
		getAlbumsFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
			return []domain.SearchResult{
				v2Album(domain.ProviderITunes, "i-fl", "FULLY LOADED", withTracks(5)),
			}, nil
		},
	}
	store := &fakeIdentityStore{mbid: "mbid-che", xref: map[string]string{"deezer": "d1", "itunes": "i1"}}
	svc := NewGetArtistContentService(
		map[domain.ProviderName]ports.ArtistContentProvider{
			domain.ProviderDeezer: deezer,
			domain.ProviderITunes: itunes,
		},
		WithContentIdentityStore(store),
	)

	for i := 0; i < 25; i++ {
		resp, err := svc.GetAlbums(context.Background(), domain.ProviderDeezer, "d1", "Che", 50)
		if err != nil {
			t.Fatalf("run %d: GetAlbums error = %v", i, err)
		}
		if len(resp.Items) != 1 {
			t.Fatalf("run %d: items = %d, want 1 merged", i, len(resp.Items))
		}
		if got := resp.Items[0].Title; got != "Fully Loaded" {
			t.Fatalf("run %d: title = %q, want %q (Deezer is the canonical incumbent every run)", i, got, "Fully Loaded")
		}
	}
}

func TestFanOutByIdentity_slowProviderCutOffAtTimeout(t *testing.T) {
	old := detailFanOutTimeout
	detailFanOutTimeout = 50 * time.Millisecond
	defer func() { detailFanOutTimeout = old }()

	fast := &fakeArtistContentProvider{
		getAlbumsFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
			return []domain.SearchResult{v2Album(domain.ProviderDeezer, "d-fl", "Fully Loaded", withDate("2026-04-01"))}, nil
		},
	}
	slow := &fakeArtistContentProvider{
		getAlbumsFn: func(ctx context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	store := &fakeIdentityStore{mbid: "mbid-che", xref: map[string]string{"deezer": "d1", "itunes": "i1"}}
	svc := NewGetArtistContentService(
		map[domain.ProviderName]ports.ArtistContentProvider{
			domain.ProviderDeezer: fast,
			domain.ProviderITunes: slow,
		},
		WithContentIdentityStore(store),
	)

	start := time.Now()
	resp, err := svc.GetAlbums(context.Background(), domain.ProviderDeezer, "d1", "Che", 50)
	if err != nil {
		t.Fatalf("GetAlbums error = %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("fan-out took %v, want the slow provider cut off near the 50ms timeout", elapsed)
	}
	if len(resp.Items) != 1 || resp.Items[0].Title != "Fully Loaded" {
		t.Fatalf("items = %+v, want just the fast provider's album (slow one degraded)", resp.Items)
	}
}

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
