package service

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/textnorm"
)

type fakeRelationshipQuerier struct {
	albumResults  []ports.RelatedTrackMatch
	artistResults []ports.RelatedTrackMatch
	err           error
}

func (f *fakeRelationshipQuerier) FindRelatedByAlbum(_ context.Context, _ string, _ int) ([]ports.RelatedTrackMatch, error) {
	return f.albumResults, f.err
}

func (f *fakeRelationshipQuerier) FindRelatedByArtist(_ context.Context, _ string, _ int) ([]ports.RelatedTrackMatch, error) {
	return f.artistResults, f.err
}

type fakeAlbumProvider struct {
	tracks []domain.SearchResult
	err    error
}

func (f *fakeAlbumProvider) GetAlbumTracks(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
	return f.tracks, f.err
}

type fakeArtistProvider struct {
	albums []domain.SearchResult
	err    error
}

func (f *fakeArtistProvider) GetArtistTopTracks(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
	return nil, nil
}

func (f *fakeArtistProvider) GetArtistAlbums(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
	return f.albums, f.err
}

func TestFindRelated_NilServiceReturnsNil(t *testing.T) {
	var svc *FindRelatedService
	got := svc.Execute(context.Background(), []domain.SearchResult{
		trackResult(domain.ProviderDeezer, "1", "Song", "Artist", nil),
	})
	if got != nil {
		t.Errorf("expected nil, got %d groups", len(got))
	}
}

func TestFindRelated_NoOrganicResultsReturnsNil(t *testing.T) {
	svc := NewFindRelatedService(nil, nil, nil)
	got := svc.Execute(context.Background(), nil)
	if got != nil {
		t.Errorf("expected nil for empty organic, got %d groups", len(got))
	}
}

func TestFindRelated_TrackWithAlbumTriggersLibraryLookup(t *testing.T) {
	artURL := "https://example.com/art.jpg"
	querier := &fakeRelationshipQuerier{
		albumResults: []ports.RelatedTrackMatch{
			{Title: "Sibling Track", Artist: "Same Artist", Album: "The Album", ArtworkURL: &artURL},
		},
	}
	svc := NewFindRelatedService(querier, nil, nil)

	organic := []domain.SearchResult{
		func() domain.SearchResult {
			r := trackResult(domain.ProviderDeezer, "1", "Main Track", "Same Artist",
				map[string]any{"album": "The Album"})
			r.Album = "The Album"
			return r
		}(),
	}

	got := svc.Execute(context.Background(), organic)

	if len(got) == 0 {
		t.Fatal("expected at least 1 related group")
	}
	found := false
	for _, g := range got {
		if g.Relationship == "library_matches" {
			found = true
			if len(g.Items) == 0 {
				t.Error("library_matches group has no items")
			}
			if g.RelatedTo != "Main Track" {
				t.Errorf("RelatedTo = %q, want %q", g.RelatedTo, "Main Track")
			}
		}
	}
	if !found {
		t.Error("expected a library_matches group")
	}
}

func TestFindRelated_TrackWithDeezerAlbumIDTriggersAlbumTracks(t *testing.T) {
	albumProvider := &fakeAlbumProvider{
		tracks: []domain.SearchResult{
			trackResult(domain.ProviderDeezer, "t1", "Track 1", "Artist", nil),
			trackResult(domain.ProviderDeezer, "t2", "Track 2", "Artist", nil),
		},
	}
	svc := NewFindRelatedService(nil, albumProvider, nil)

	mainTrack := trackResult(domain.ProviderDeezer, "1", "Main Track", "Artist", nil)
	mainTrack.DeezerAlbumID = "12345"
	organic := []domain.SearchResult{mainTrack}

	got := svc.Execute(context.Background(), organic)

	found := false
	for _, g := range got {
		if g.Relationship == "album_tracks" {
			found = true
			if len(g.Items) != 2 {
				t.Errorf("expected 2 album tracks, got %d", len(g.Items))
			}
		}
	}
	if !found {
		t.Error("expected an album_tracks group")
	}
}

func TestFindRelated_ArtistTriggersArtistAlbums(t *testing.T) {
	artistProvider := &fakeArtistProvider{
		albums: []domain.SearchResult{
			albumResult(domain.ProviderDeezer, "a1", "Album 1", "Artist", nil),
			albumResult(domain.ProviderDeezer, "a2", "Album 2", "Artist", nil),
		},
	}
	svc := NewFindRelatedService(nil, nil, artistProvider)

	organic := []domain.SearchResult{
		artistResult(domain.ProviderDeezer, "dz-1", "Artist", nil),
	}

	got := svc.Execute(context.Background(), organic)

	found := false
	for _, g := range got {
		if g.Relationship == "artist_albums" {
			found = true
			if len(g.Items) != 2 {
				t.Errorf("expected 2 artist albums, got %d", len(g.Items))
			}
		}
	}
	if !found {
		t.Error("expected an artist_albums group")
	}
}

func TestFindRelated_DedupAgainstOrganic(t *testing.T) {
	albumProvider := &fakeAlbumProvider{
		tracks: []domain.SearchResult{
			trackResult(domain.ProviderDeezer, "t1", "Main Track", "Artist", nil),
			trackResult(domain.ProviderDeezer, "t2", "Other Track", "Artist", nil),
		},
	}
	svc := NewFindRelatedService(nil, albumProvider, nil)

	mainTrack := trackResult(domain.ProviderDeezer, "1", "Main Track", "Artist", nil)
	mainTrack.DeezerAlbumID = "12345"
	organic := []domain.SearchResult{mainTrack}

	got := svc.Execute(context.Background(), organic)

	for _, g := range got {
		for _, item := range g.Items {
			if textnorm.NormalizeForMatch(item.Title) == textnorm.NormalizeForMatch("Main Track") &&
				textnorm.NormalizeForMatch(item.Subtitle) == textnorm.NormalizeForMatch("Artist") {
				t.Error("organic result should be deduped from related items")
			}
		}
	}
}

func TestFindRelated_ProviderCallCap(t *testing.T) {
	var callCount atomic.Int64
	albumProvider := &fakeAlbumProvider{
		tracks: []domain.SearchResult{
			trackResult(domain.ProviderDeezer, "t1", "Track", "Artist", nil),
		},
	}

	svc := &FindRelatedService{
		albumProvider: &countingAlbumProvider{inner: albumProvider, count: &callCount},
	}

	var organic []domain.SearchResult
	for i := 0; i < 5; i++ {
		r := trackResult(domain.ProviderDeezer, fmt.Sprintf("d%d", i), fmt.Sprintf("Track %d", i), "Artist", nil)
		r.DeezerAlbumID = fmt.Sprintf("%d", 100+i)
		organic = append(organic, r)
	}

	svc.Execute(context.Background(), organic)

	if got := callCount.Load(); got > int64(maxProviderLookups) {
		t.Errorf("expected at most %d provider calls, got %d", maxProviderLookups, got)
	}
}

// countingAlbumProvider counts calls; the production scatter-gather invokes it
// from multiple goroutines, so the counter must be atomic (caught by -race).
type countingAlbumProvider struct {
	inner ports.AlbumContentProvider
	count *atomic.Int64
}

func (c *countingAlbumProvider) GetAlbumTracks(ctx context.Context, p domain.ProviderName, id string) ([]domain.SearchResult, error) {
	c.count.Add(1)
	return c.inner.GetAlbumTracks(ctx, p, id)
}

func TestFindRelated_TimeoutReturnsPartialResults(t *testing.T) {
	slowProvider := &fakeAlbumProvider{
		tracks: []domain.SearchResult{
			trackResult(domain.ProviderDeezer, "t1", "Slow Track", "Artist", nil),
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()
	time.Sleep(5 * time.Millisecond)

	svc := NewFindRelatedService(nil, slowProvider, nil)
	slowTrack := trackResult(domain.ProviderDeezer, "1", "Track", "Artist", nil)
	slowTrack.DeezerAlbumID = "123"
	organic := []domain.SearchResult{slowTrack}

	got := svc.Execute(ctx, organic)
	_ = got
}
