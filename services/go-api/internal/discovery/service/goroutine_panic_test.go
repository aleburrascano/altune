package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"
	"context"
	"testing"
)

// These tests guard issue #568: every goroutine the discovery module spawns
// around a provider or port call must contain a panic. An unrecovered panic in
// a child goroutine terminates the whole process (net/http only recovers the
// request goroutine), so without recovery each test below crashes the test
// binary instead of failing.

type panickingArtworkResolver struct{ fakeArtworkResolver }

func (panickingArtworkResolver) ResolveTagged(context.Context, domain.ResultKind, string, string, string) (string, domain.ProviderKey, error) {
	panic("artwork resolver exploded")
}

func TestFillArtwork_PanickingResolverIsContained(t *testing.T) {
	p := &fakeProvider{name: domain.ProviderDeezer, results: []domain.SearchResult{deezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := NewService([]ports.SearchProvider{p}, NewCircuitBreaker(), WithArtworkResolver(&panickingArtworkResolver{}))

	out := runSearch(t, svc, "humble")

	if len(out.Results) != 1 || out.Results[0].Title != "Humble" {
		t.Fatalf("results = %+v, want the original Humble result kept", out.Results)
	}
}

type panickingQuerier struct{}

func (panickingQuerier) FindRelatedByAlbum(context.Context, shared.UserId, string, int) ([]ports.RelatedTrackMatch, error) {
	panic("querier exploded")
}

type panickingAlbumProvider struct{}

func (panickingAlbumProvider) GetAlbumTracks(context.Context, domain.ProviderName, string) ([]domain.SearchResult, error) {
	panic("album provider exploded")
}

type panickingArtistProvider struct{}

func (panickingArtistProvider) GetArtistTopTracks(context.Context, domain.ProviderName, string) ([]domain.SearchResult, error) {
	panic("artist provider exploded")
}

func (panickingArtistProvider) GetArtistAlbums(context.Context, domain.ProviderName, string) ([]domain.SearchResult, error) {
	panic("artist provider exploded")
}

func TestFindRelated_PanickingDependenciesAreContained(t *testing.T) {
	svc := NewFindRelatedService(panickingQuerier{}, panickingAlbumProvider{}, panickingArtistProvider{})

	mainTrack := trackResult(domain.ProviderDeezer, "1", "Main Track", "Artist", nil)
	mainTrack.Album = "Some Album"
	mainTrack.DeezerAlbumID = "12345"
	organic := []domain.SearchResult{
		mainTrack,
		artistResult(domain.ProviderDeezer, "dz-1", "Artist", nil),
	}

	if got := svc.Execute(context.Background(), newUser(), organic); len(got) != 0 {
		t.Errorf("groups = %+v, want none when every lookup panics", got)
	}
}

func TestFindRelated_PanicInOneLookupKeepsTheOthers(t *testing.T) {
	artistProvider := &fakeArtistProvider{albums: []domain.SearchResult{
		albumResult(domain.ProviderDeezer, "a1", "Album 1", "Artist", nil),
	}}
	svc := NewFindRelatedService(nil, panickingAlbumProvider{}, artistProvider)

	mainTrack := trackResult(domain.ProviderDeezer, "1", "Main Track", "Artist", nil)
	mainTrack.DeezerAlbumID = "12345"
	organic := []domain.SearchResult{
		mainTrack,
		artistResult(domain.ProviderDeezer, "dz-1", "Artist", nil),
	}

	got := svc.Execute(context.Background(), newUser(), organic)
	if len(got) != 1 || got[0].Relationship != domain.RelationshipArtistAlbums {
		t.Errorf("groups = %+v, want only the artist_albums group", got)
	}
}

func TestFanOutByIdentity_PanickingFetchIsContained(t *testing.T) {
	svc := NewGetArtistContentService(map[domain.ProviderName]ports.ArtistContentProvider{
		domain.ProviderDeezer: &fakeArtistContentProvider{},
		domain.ProviderITunes: &fakeArtistContentProvider{},
	})
	identity := ResolvedArtistIdentity{ProviderIDs: map[domain.ProviderName]string{
		domain.ProviderDeezer: "dz",
		domain.ProviderITunes: "it",
	}}
	fetch := func(_ context.Context, _ ports.ArtistContentProvider, provider domain.ProviderName, _ string) ([]domain.SearchResult, error) {
		if provider == domain.ProviderDeezer {
			panic("deezer exploded")
		}
		return []domain.SearchResult{trackResult(provider, "t1", "Track", "Artist", nil)}, nil
	}

	groups, partial := svc.fanOutByIdentity(context.Background(), identity, "Artist", fetch)

	if len(groups) != 1 {
		t.Fatalf("groups = %d, want 1 (the non-panicking provider)", len(groups))
	}
	if !partial {
		t.Error("partial = false, want true (the panicking provider failed to answer)")
	}
}

func TestFanOutConsensus_PanickingCollectIsContained(t *testing.T) {
	providers := []ConsensusProvider{{Name: "boom"}, {Name: "ok"}}
	out := FanOutConsensus(context.Background(), providers, func(_ context.Context, p ConsensusProvider) int {
		if p.Name == "boom" {
			panic("collect exploded")
		}
		return 7
	})

	if out["ok"] != 7 {
		t.Errorf("ok = %d, want 7", out["ok"])
	}
	if _, present := out["boom"]; present {
		t.Errorf("panicking provider should be absent, got %v", out["boom"])
	}
}

type panickingTrackFeatured struct{ fakeTrackFeatured }

func (panickingTrackFeatured) LookupTrackFeatured(context.Context, string) ([]domain.FeaturedArtist, error) {
	panic("featured lookup exploded")
}

func TestEnrichFeatured_PanickingLookupIsContained(t *testing.T) {
	svc := NewGetAlbumTracksService(nil, WithTrackFeatured(panickingTrackFeatured{}))
	results := []domain.SearchResult{deezerTrackFeat("1", "Singapore")}

	svc.enrichFeatured(context.Background(), results)

	if _, present := results[0].Extras["featured_artists"]; present {
		t.Errorf("featured should be unset after a panicking lookup, got %v", results[0].Extras["featured_artists"])
	}
}
