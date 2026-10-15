package related

import (
	"altune/go-api/internal/discovery/discoverytest"
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/discovery/service/providercall"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/textnorm"
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeRelationshipQuerier struct {
	albumResults  []ports.RelatedTrackMatch
	artistResults []ports.RelatedTrackMatch
	err           error
}

func (f *fakeRelationshipQuerier) FindRelatedByAlbumArtist(_ context.Context, _ shared.UserId, _ string, _ string, _ int) ([]ports.RelatedTrackMatch, error) {
	return f.albumResults, f.err
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
	got := svc.Execute(context.Background(), discoverytest.NewUser(), []domain.SearchResult{
		discoverytest.TrackResult(domain.ProviderDeezer, "1", "Song", "Artist", nil),
	})
	if got != nil {
		t.Errorf("expected nil, got %d groups", len(got))
	}
}

func TestFindRelated_NoOrganicResultsReturnsNil(t *testing.T) {
	svc := NewFindRelatedService(nil, nil, nil)
	got := svc.Execute(context.Background(), discoverytest.NewUser(), nil)
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
			r := discoverytest.TrackResult(domain.ProviderDeezer, "1", "Main Track", "Same Artist",
				map[string]any{"album": "The Album"})
			r.Album = "The Album"
			return r
		}(),
	}

	got := svc.Execute(context.Background(), discoverytest.NewUser(), organic)

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

func TestFindRelated_LibraryArtworkOnlyKeepsHTTPS(t *testing.T) {
	cases := map[string]string{
		"https://example.com/art.jpg": "https://example.com/art.jpg",
		"http://example.com/art.jpg":  "",
		"javascript:alert(1)":         "",
	}
	for artwork, want := range cases {
		t.Run(artwork, func(t *testing.T) {
			art := artwork
			querier := &fakeRelationshipQuerier{
				albumResults: []ports.RelatedTrackMatch{
					{Title: "Sibling Track", Artist: "Same Artist", Album: "The Album", ArtworkURL: &art},
				},
			}
			svc := NewFindRelatedService(querier, nil, nil)
			main := discoverytest.TrackResult(domain.ProviderDeezer, "1", "Main Track", "Same Artist", nil)
			main.Album = "The Album"

			got := svc.Execute(context.Background(), discoverytest.NewUser(), []domain.SearchResult{main})

			for _, g := range got {
				if g.Relationship == "library_matches" {
					if len(g.Items) != 1 || g.Items[0].ImageURL != want {
						t.Fatalf("items = %+v, want one item with ImageURL %q", g.Items, want)
					}
					return
				}
			}
			t.Fatal("expected a library_matches group")
		})
	}
}

func TestFindRelated_TrackWithDeezerAlbumIDTriggersAlbumTracks(t *testing.T) {
	albumProvider := &fakeAlbumProvider{
		tracks: []domain.SearchResult{
			discoverytest.TrackResult(domain.ProviderDeezer, "t1", "Track 1", "Artist", nil),
			discoverytest.TrackResult(domain.ProviderDeezer, "t2", "Track 2", "Artist", nil),
		},
	}
	svc := NewFindRelatedService(nil, albumProvider, nil)

	mainTrack := discoverytest.TrackResult(domain.ProviderDeezer, "1", "Main Track", "Artist", nil)
	mainTrack.DeezerAlbumID = "12345"
	organic := []domain.SearchResult{mainTrack}

	got := svc.Execute(context.Background(), discoverytest.NewUser(), organic)

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
			discoverytest.AlbumResult(domain.ProviderDeezer, "a1", "Album 1", "Artist", nil),
			discoverytest.AlbumResult(domain.ProviderDeezer, "a2", "Album 2", "Artist", nil),
		},
	}
	svc := NewFindRelatedService(nil, nil, artistProvider)

	organic := []domain.SearchResult{
		discoverytest.ArtistResult(domain.ProviderDeezer, "dz-1", "Artist", nil),
	}

	got := svc.Execute(context.Background(), discoverytest.NewUser(), organic)

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
			discoverytest.TrackResult(domain.ProviderDeezer, "t1", "Main Track", "Artist", nil),
			discoverytest.TrackResult(domain.ProviderDeezer, "t2", "Other Track", "Artist", nil),
		},
	}
	svc := NewFindRelatedService(nil, albumProvider, nil)

	mainTrack := discoverytest.TrackResult(domain.ProviderDeezer, "1", "Main Track", "Artist", nil)
	mainTrack.DeezerAlbumID = "12345"
	organic := []domain.SearchResult{mainTrack}

	got := svc.Execute(context.Background(), discoverytest.NewUser(), organic)

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
			discoverytest.TrackResult(domain.ProviderDeezer, "t1", "Track", "Artist", nil),
		},
	}

	svc := &FindRelatedService{
		albumProvider: &countingAlbumProvider{inner: albumProvider, count: &callCount},
	}

	var organic []domain.SearchResult
	for i := 0; i < 5; i++ {
		r := discoverytest.TrackResult(domain.ProviderDeezer, fmt.Sprintf("d%d", i), fmt.Sprintf("Track %d", i), "Artist", nil)
		r.DeezerAlbumID = fmt.Sprintf("%d", 100+i)
		organic = append(organic, r)
	}

	svc.Execute(context.Background(), discoverytest.NewUser(), organic)

	if got := callCount.Load(); got > int64(maxProviderLookups) {
		t.Errorf("expected at most %d provider calls, got %d", maxProviderLookups, got)
	}
}

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
			discoverytest.TrackResult(domain.ProviderDeezer, "t1", "Slow Track", "Artist", nil),
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()
	time.Sleep(5 * time.Millisecond)

	svc := NewFindRelatedService(nil, slowProvider, nil)
	slowTrack := discoverytest.TrackResult(domain.ProviderDeezer, "1", "Track", "Artist", nil)
	slowTrack.DeezerAlbumID = "123"
	organic := []domain.SearchResult{slowTrack}

	got := svc.Execute(ctx, discoverytest.NewUser(), organic)
	_ = got
}

type scriptedQuerier struct {
	calls   atomic.Int32
	matches func(album string) ([]ports.RelatedTrackMatch, error)
	sawCtx  func(ctx context.Context)
}

func (q *scriptedQuerier) FindRelatedByAlbumArtist(ctx context.Context, _ shared.UserId, album string, _ string, _ int) ([]ports.RelatedTrackMatch, error) {
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
		out = append(out, discoverytest.TrackResult(domain.ProviderDeezer, fmt.Sprintf("%s-%d", prefix, i),
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
	r := discoverytest.TrackResult(domain.ProviderDeezer, fmt.Sprintf("org-%d", i), fmt.Sprintf("Organic %d", i), "Organic Artist", nil)
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
		discoverytest.ArtistResult(domain.ProviderDeezer, "dz-artist", "Organic Artist Name", nil),
	}
	byKind := groupsByKind(svc.Execute(context.Background(), discoverytest.NewUser(), organic))

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

func TestFindRelatedDispatch_LibraryLookupsNotCountedAgainstProviderBudget(t *testing.T) {
	querier := &scriptedQuerier{matches: func(album string) ([]ports.RelatedTrackMatch, error) {
		return []ports.RelatedTrackMatch{{Title: "Lib " + album, Artist: "Lib Artist"}}, nil
	}}
	albums := &scriptedAlbumProvider{tracks: func(id string) ([]domain.SearchResult, error) {
		return manyResults(id, 1), nil
	}}
	svc := NewFindRelatedService(querier, albums, nil)

	organic := make([]domain.SearchResult, 0, RelatedTopN+2)
	for i := range RelatedTopN + 2 {
		organic = append(organic, trackWithAlbum(i))
	}
	byKind := groupsByKind(svc.Execute(context.Background(), discoverytest.NewUser(), organic))

	if got := querier.calls.Load(); got != int32(RelatedTopN) {
		t.Errorf("library lookups = %d, want %d", got, RelatedTopN)
	}
	if got := albums.calls.Load(); got != int32(maxProviderLookups) {
		t.Errorf("album-tracks lookups = %d, want %d", got, maxProviderLookups)
	}
	if got := len(byKind[domain.RelationshipLibraryMatches]); got != RelatedTopN {
		t.Errorf("library groups = %d, want %d", got, RelatedTopN)
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
		return nil, nil
	}}
	svc := NewFindRelatedService(querier, albums, artists)

	organic := []domain.SearchResult{
		trackWithAlbum(0),
		trackWithAlbum(1),
		discoverytest.ArtistResult(domain.ProviderDeezer, "dz-artist", "Some Artist", nil),
	}
	got := svc.Execute(context.Background(), discoverytest.NewUser(), organic)

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
		discoverytest.ArtistResult(domain.ProviderDeezer, "dz-artist", "Some Artist", nil),
	}
	got := svc.Execute(context.Background(), discoverytest.NewUser(), organic)

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
	svc.Execute(parent, discoverytest.NewUser(), []domain.SearchResult{trackWithAlbum(0)})

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
	svc.Execute(cancelled, discoverytest.NewUser(), []domain.SearchResult{trackWithAlbum(0)})
	mu.Lock()
	defer mu.Unlock()
	if !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled caller: lookup ctx err = %v, want context.Canceled", err)
	}
}

type panickingQuerier struct{}

func (panickingQuerier) FindRelatedByAlbumArtist(context.Context, shared.UserId, string, string, int) ([]ports.RelatedTrackMatch, error) {
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

	mainTrack := discoverytest.TrackResult(domain.ProviderDeezer, "1", "Main Track", "Artist", nil)
	mainTrack.Album = "Some Album"
	mainTrack.DeezerAlbumID = "12345"
	organic := []domain.SearchResult{
		mainTrack,
		discoverytest.ArtistResult(domain.ProviderDeezer, "dz-1", "Artist", nil),
	}

	if got := svc.Execute(context.Background(), discoverytest.NewUser(), organic); len(got) != 0 {
		t.Errorf("groups = %+v, want none when every lookup panics", got)
	}
}

func TestFindRelated_PanicInOneLookupKeepsTheOthers(t *testing.T) {
	artistProvider := &fakeArtistProvider{albums: []domain.SearchResult{
		discoverytest.AlbumResult(domain.ProviderDeezer, "a1", "Album 1", "Artist", nil),
	}}
	svc := NewFindRelatedService(nil, panickingAlbumProvider{}, artistProvider)

	mainTrack := discoverytest.TrackResult(domain.ProviderDeezer, "1", "Main Track", "Artist", nil)
	mainTrack.DeezerAlbumID = "12345"
	organic := []domain.SearchResult{
		mainTrack,
		discoverytest.ArtistResult(domain.ProviderDeezer, "dz-1", "Artist", nil),
	}

	got := svc.Execute(context.Background(), discoverytest.NewUser(), organic)
	if len(got) != 1 || got[0].Relationship != domain.RelationshipArtistAlbums {
		t.Errorf("groups = %+v, want only the artist_albums group", got)
	}
}

func TestFindRelated_PartialArtistAlbumsStillYieldsGroup(t *testing.T) {
	artistProvider := &fakeArtistProvider{
		albums: []domain.SearchResult{discoverytest.AlbumResult(domain.ProviderDeezer, "a1", "Album 1", "Artist", nil)},
		err:    &domain.PartialResultError{Page: 2, Err: errors.New("deezer 500")},
	}
	svc := NewFindRelatedService(nil, nil, artistProvider)

	got := svc.Execute(context.Background(), discoverytest.NewUser(), []domain.SearchResult{
		discoverytest.ArtistResult(domain.ProviderDeezer, "dz-1", "Artist", nil),
	})

	if len(got) != 1 || got[0].Relationship != "artist_albums" || len(got[0].Items) != 1 {
		t.Errorf("groups = %+v, want one artist_albums group with the partial album", got)
	}
}

func TestRelatedMemo_ItemsExpireAfterTTLOnTheInjectedClock(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	memo := newRelatedMemo(time.Hour, func() time.Time { return now })
	key := relatedMemoKey{subject: "humble"}
	memo.remember(key, []domain.SearchResult{discoverytest.DeezerTrack("Humble", "Kendrick Lamar", 80)})

	now = now.Add(time.Hour - time.Second)
	if _, memoized := memo.items(key); !memoized {
		t.Fatal("entry inside its ttl was not served")
	}

	now = now.Add(2 * time.Second)
	if _, memoized := memo.items(key); memoized {
		t.Error("entry past its ttl was still served")
	}
}

type breakerProbeAlbumProvider struct {
	calls atomic.Int32
	err   error
}

func (c *breakerProbeAlbumProvider) GetAlbumTracks(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
	c.calls.Add(1)
	return nil, c.err
}

func relatedAlbumTrackQuery() []domain.SearchResult {
	track := discoverytest.TrackResult(domain.ProviderDeezer, "1", "Main Track", "Artist", nil)
	track.DeezerAlbumID = "12345"
	return []domain.SearchResult{track}
}

func TestFindRelated_OpenDeezerBreakerSkipsProviderCall(t *testing.T) {
	cb := providercall.NewCircuitBreaker()
	for range providercall.FailureThreshold {
		cb.RecordFailure(context.Background(), domain.ProviderDeezer)
	}
	provider := &breakerProbeAlbumProvider{}
	svc := NewFindRelatedService(nil, provider, nil, WithFindRelatedCircuitBreaker(cb))

	got := svc.Execute(context.Background(), discoverytest.NewUser(), relatedAlbumTrackQuery())

	if provider.calls.Load() != 0 || len(got) != 0 {
		t.Errorf("provider calls = %d, groups = %d; want 0, 0", provider.calls.Load(), len(got))
	}
}

func TestFindRelated_DeezerFailureRecordsOnSharedBreaker(t *testing.T) {
	cb := providercall.NewCircuitBreaker()
	provider := &breakerProbeAlbumProvider{err: context.DeadlineExceeded}
	svc := NewFindRelatedService(nil, provider, nil, WithFindRelatedCircuitBreaker(cb))

	for range providercall.FailureThreshold {
		svc.Execute(context.Background(), discoverytest.NewUser(), relatedAlbumTrackQuery())
	}
	svc.Execute(context.Background(), discoverytest.NewUser(), relatedAlbumTrackQuery())

	if provider.calls.Load() != providercall.FailureThreshold {
		t.Errorf("provider calls = %d, want %d then circuit open", provider.calls.Load(), providercall.FailureThreshold)
	}
}

type libraryByArtist map[string][]ports.RelatedTrackMatch

func (l libraryByArtist) FindRelatedByAlbumArtist(_ context.Context, _ shared.UserId, _ string, artist string, _ int) ([]ports.RelatedTrackMatch, error) {
	return l[artist], nil
}

func TestFindRelated_SameAlbumNameOfAnotherArtistIsNotRelated(t *testing.T) {
	querier := libraryByArtist{
		"Queen": {{Title: "Bohemian Rhapsody", Artist: "Queen", Album: "Greatest Hits"}},
		"ABBA":  {{Title: "Dancing Queen", Artist: "ABBA", Album: "Greatest Hits"}},
	}
	svc := NewFindRelatedService(querier, nil, nil)
	user := discoverytest.NewUser()
	hit := func(artist string) []domain.SearchResult {
		r := discoverytest.TrackResult(domain.ProviderDeezer, "1", "Main", artist, nil)
		r.Album = "Greatest Hits"
		return []domain.SearchResult{r}
	}

	queen := svc.Execute(context.Background(), user, hit("Queen"))
	abba := svc.Execute(context.Background(), user, hit("ABBA"))

	if len(queen) != 1 || len(queen[0].Items) != 1 || queen[0].Items[0].Title != "Bohemian Rhapsody" {
		t.Errorf("Queen related = %+v, want only Bohemian Rhapsody", queen)
	}
	if len(abba) != 1 || len(abba[0].Items) != 1 || abba[0].Items[0].Title != "Dancing Queen" {
		t.Errorf("ABBA related = %+v, want only Dancing Queen (memo must not cross artists)", abba)
	}
}
