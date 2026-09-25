package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// These tests pin FindRelatedService.Execute's fan-out semantics: per-kind
// truncation, which lookups count against the provider budget, partial
// failure, panic isolation, and context propagation.

type scriptedQuerier struct {
	calls   atomic.Int32
	matches func(album string) ([]ports.RelatedTrackMatch, error)
	sawCtx  func(ctx context.Context)
}

func (q *scriptedQuerier) FindRelatedByAlbum(ctx context.Context, _ shared.UserId, album string, _ int) ([]ports.RelatedTrackMatch, error) {
	q.calls.Add(1)
	if q.sawCtx != nil {
		q.sawCtx(ctx)
	}
	return q.matches(album)
}

type scriptedAlbumProvider struct {
	calls  atomic.Int32
	tracks func(albumID string) ([]domain.SearchResult, error)
}

func (p *scriptedAlbumProvider) GetAlbumTracks(_ context.Context, _ domain.ProviderName, albumID string) ([]domain.SearchResult, error) {
	p.calls.Add(1)
	return p.tracks(albumID)
}

type scriptedArtistProvider struct {
	calls  atomic.Int32
	albums func(artistID string) ([]domain.SearchResult, error)
}

func (p *scriptedArtistProvider) GetArtistTopTracks(context.Context, domain.ProviderName, string) ([]domain.SearchResult, error) {
	return nil, nil
}

func (p *scriptedArtistProvider) GetArtistAlbums(_ context.Context, _ domain.ProviderName, artistID string) ([]domain.SearchResult, error) {
	p.calls.Add(1)
	return p.albums(artistID)
}

func manyResults(prefix string, n int) []domain.SearchResult {
	out := make([]domain.SearchResult, 0, n)
	for i := range n {
		out = append(out, trackResult(domain.ProviderDeezer, fmt.Sprintf("%s-%d", prefix, i),
			fmt.Sprintf("%s item %d", prefix, i), prefix+" artist", nil))
	}
	return out
}

func groupsByKind(groups []domain.RelatedGroup) map[domain.RelationshipKind][]domain.RelatedGroup {
	out := map[domain.RelationshipKind][]domain.RelatedGroup{}
	for _, g := range groups {
		out[g.Relationship] = append(out[g.Relationship], g)
	}
	return out
}

func trackWithAlbum(i int) domain.SearchResult {
	r := trackResult(domain.ProviderDeezer, fmt.Sprintf("org-%d", i), fmt.Sprintf("Organic %d", i), "Organic Artist", nil)
	r.Album = fmt.Sprintf("Album %d", i)
	r.DeezerAlbumID = fmt.Sprintf("dz-album-%d", i)
	return r
}

func TestFindRelatedDispatch_ProviderGroupsTruncatedLibraryGroupsNot(t *testing.T) {
	querier := &scriptedQuerier{matches: func(string) ([]ports.RelatedTrackMatch, error) {
		m := make([]ports.RelatedTrackMatch, 0, relatedPerGroup+3)
		for i := range relatedPerGroup + 3 {
			m = append(m, ports.RelatedTrackMatch{Title: fmt.Sprintf("Lib %d", i), Artist: "Lib Artist"})
		}
		return m, nil
	}}
	albums := &scriptedAlbumProvider{tracks: func(string) ([]domain.SearchResult, error) {
		return manyResults("albumtrack", relatedPerGroup+5), nil
	}}
	artists := &scriptedArtistProvider{albums: func(string) ([]domain.SearchResult, error) {
		return manyResults("artistalbum", relatedPerGroup+5), nil
	}}
	svc := NewFindRelatedService(querier, albums, artists)

	organic := []domain.SearchResult{
		trackWithAlbum(0),
		artistResult(domain.ProviderDeezer, "dz-artist", "Organic Artist Name", nil),
	}
	byKind := groupsByKind(svc.Execute(context.Background(), newUser(), organic))

	assertGroup := func(kind domain.RelationshipKind, relatedTo string, wantItems int) {
		t.Helper()
		gs := byKind[kind]
		if len(gs) != 1 {
			t.Fatalf("%s: got %d groups, want 1", kind, len(gs))
		}
		if gs[0].RelatedTo != relatedTo {
			t.Errorf("%s: RelatedTo = %q, want %q", kind, gs[0].RelatedTo, relatedTo)
		}
		if len(gs[0].Items) != wantItems {
			t.Errorf("%s: got %d items, want %d", kind, len(gs[0].Items), wantItems)
		}
	}
	assertGroup(domain.RelationshipLibraryMatches, "Organic 0", relatedPerGroup+3)
	assertGroup(domain.RelationshipAlbumTracks, "Organic 0", relatedPerGroup)
	assertGroup(domain.RelationshipArtistAlbums, "Organic Artist Name", relatedPerGroup)
}

// Library lookups do not draw from the provider-call budget: with a full
// top-N of tracks carrying both an album name and a Deezer album ID, every
// library lookup and every album-tracks lookup runs.
func TestFindRelatedDispatch_LibraryLookupsNotCountedAgainstProviderBudget(t *testing.T) {
	querier := &scriptedQuerier{matches: func(album string) ([]ports.RelatedTrackMatch, error) {
		return []ports.RelatedTrackMatch{{Title: "Lib " + album, Artist: "Lib Artist"}}, nil
	}}
	albums := &scriptedAlbumProvider{tracks: func(id string) ([]domain.SearchResult, error) {
		return manyResults(id, 1), nil
	}}
	svc := NewFindRelatedService(querier, albums, nil)

	organic := make([]domain.SearchResult, 0, relatedTopN+2)
	for i := range relatedTopN + 2 {
		organic = append(organic, trackWithAlbum(i))
	}
	byKind := groupsByKind(svc.Execute(context.Background(), newUser(), organic))

	if got := querier.calls.Load(); got != int32(relatedTopN) {
		t.Errorf("library lookups = %d, want %d", got, relatedTopN)
	}
	if got := albums.calls.Load(); got != int32(maxProviderLookups) {
		t.Errorf("album-tracks lookups = %d, want %d", got, maxProviderLookups)
	}
	if got := len(byKind[domain.RelationshipLibraryMatches]); got != relatedTopN {
		t.Errorf("library groups = %d, want %d", got, relatedTopN)
	}
	if got := len(byKind[domain.RelationshipAlbumTracks]); got != maxProviderLookups {
		t.Errorf("album-tracks groups = %d, want %d", got, maxProviderLookups)
	}
}

func TestFindRelatedDispatch_OneFailingLookupDropsOnlyItsGroup(t *testing.T) {
	querier := &scriptedQuerier{matches: func(string) ([]ports.RelatedTrackMatch, error) {
		return nil, errors.New("db down")
	}}
	albums := &scriptedAlbumProvider{tracks: func(id string) ([]domain.SearchResult, error) {
		if id == "dz-album-1" {
			return nil, errors.New("deezer 500")
		}
		return manyResults(id, 2), nil
	}}
	artists := &scriptedArtistProvider{albums: func(string) ([]domain.SearchResult, error) {
		return nil, nil // empty result: no group
	}}
	svc := NewFindRelatedService(querier, albums, artists)

	organic := []domain.SearchResult{
		trackWithAlbum(0),
		trackWithAlbum(1),
		artistResult(domain.ProviderDeezer, "dz-artist", "Some Artist", nil),
	}
	got := svc.Execute(context.Background(), newUser(), organic)

	if len(got) != 1 {
		t.Fatalf("got %d groups, want 1: %+v", len(got), got)
	}
	if got[0].Relationship != domain.RelationshipAlbumTracks || got[0].RelatedTo != "Organic 0" {
		t.Errorf("got %s for %q, want album_tracks for %q", got[0].Relationship, got[0].RelatedTo, "Organic 0")
	}
}

func TestFindRelatedDispatch_PanicInOneLookupIsIsolated(t *testing.T) {
	querier := &scriptedQuerier{matches: func(string) ([]ports.RelatedTrackMatch, error) {
		panic("querier exploded")
	}}
	albums := &scriptedAlbumProvider{tracks: func(string) ([]domain.SearchResult, error) {
		panic("album provider exploded")
	}}
	artists := &scriptedArtistProvider{albums: func(id string) ([]domain.SearchResult, error) {
		return manyResults(id, 1), nil
	}}
	svc := NewFindRelatedService(querier, albums, artists)

	organic := []domain.SearchResult{
		trackWithAlbum(0),
		artistResult(domain.ProviderDeezer, "dz-artist", "Some Artist", nil),
	}
	got := svc.Execute(context.Background(), newUser(), organic)

	if len(got) != 1 || got[0].Relationship != domain.RelationshipArtistAlbums {
		t.Fatalf("want only the artist_albums group, got %+v", got)
	}
}

func TestFindRelatedDispatch_LookupsReceiveBoundedCallerContext(t *testing.T) {
	type ctxKey struct{}
	var (
		mu       sync.Mutex
		deadline time.Time
		hadDL    bool
		value    any
		err      error
	)
	querier := &scriptedQuerier{
		matches: func(string) ([]ports.RelatedTrackMatch, error) { return nil, nil },
		sawCtx: func(ctx context.Context) {
			mu.Lock()
			defer mu.Unlock()
			deadline, hadDL = ctx.Deadline()
			value = ctx.Value(ctxKey{})
			err = ctx.Err()
		},
	}
	svc := NewFindRelatedService(querier, nil, nil)

	parent := context.WithValue(context.Background(), ctxKey{}, "marker")
	start := time.Now()
	svc.Execute(parent, newUser(), []domain.SearchResult{trackWithAlbum(0)})

	if querier.calls.Load() != 1 {
		t.Fatalf("library lookups = %d, want 1", querier.calls.Load())
	}
	if !hadDL || deadline.After(start.Add(relatedTimeout+time.Second)) {
		t.Errorf("lookup ctx deadline = %v (set=%v), want within %v of start", deadline, hadDL, relatedTimeout)
	}
	if value != "marker" {
		t.Errorf("lookup ctx lost caller values: got %v", value)
	}
	if err != nil {
		t.Errorf("lookup ctx already done: %v", err)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	svc.Execute(cancelled, newUser(), []domain.SearchResult{trackWithAlbum(0)})
	mu.Lock()
	defer mu.Unlock()
	if !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled caller: lookup ctx err = %v, want context.Canceled", err)
	}
}
