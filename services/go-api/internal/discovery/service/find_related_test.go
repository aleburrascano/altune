package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/textnorm"
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

type fakeRelationshipQuerier struct {
	albumResults  []ports.RelatedTrackMatch
	artistResults []ports.RelatedTrackMatch
	err           error
}

func (f *fakeRelationshipQuerier) FindRelatedByAlbum(_ context.Context, _ shared.UserId, _ string, _ int) ([]ports.RelatedTrackMatch, error) {
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
	got := svc.Execute(context.Background(), newUser(), []domain.SearchResult{
		trackResult(domain.ProviderDeezer, "1", "Song", "Artist", nil),
	})
	if got != nil {
		t.Errorf("expected nil, got %d groups", len(got))
	}
}

func TestFindRelated_NoOrganicResultsReturnsNil(t *testing.T) {
	svc := NewFindRelatedService(nil, nil, nil)
	got := svc.Execute(context.Background(), newUser(), nil)
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

	got := svc.Execute(context.Background(), newUser(), organic)

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

	got := svc.Execute(context.Background(), newUser(), organic)

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

	got := svc.Execute(context.Background(), newUser(), organic)

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

	got := svc.Execute(context.Background(), newUser(), organic)

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

	svc.Execute(context.Background(), newUser(), organic)

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

	got := svc.Execute(ctx, newUser(), organic)
	_ = got
}

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

type countingRelationshipQuerier struct {
	matchesByUser map[shared.UserId][]ports.RelatedTrackMatch
	calls         atomic.Int64
}

func (q *countingRelationshipQuerier) FindRelatedByAlbum(_ context.Context, userId shared.UserId, _ string, _ int) ([]ports.RelatedTrackMatch, error) {
	q.calls.Add(1)
	return q.matchesByUser[userId], nil
}

type countingAlbumTracksProvider struct {
	tracksByAlbum map[string][]domain.SearchResult
	calls         atomic.Int64
}

func (p *countingAlbumTracksProvider) GetAlbumTracks(_ context.Context, _ domain.ProviderName, albumID string) ([]domain.SearchResult, error) {
	p.calls.Add(1)
	return p.tracksByAlbum[albumID], nil
}

// relatedSearchFixture is a slate of tracks whose titles all match the query, so
// ranking keeps every one of them in the top relatedTopN, each on its own album
// so each draws its own library query and provider call.
func relatedSearchFixture() ([]domain.SearchResult, map[string][]domain.SearchResult) {
	var organic []domain.SearchResult
	tracksByAlbum := map[string][]domain.SearchResult{}
	for i := 0; i < relatedTopN; i++ {
		albumID := fmt.Sprintf("al-%d", i)
		track := trackResult(domain.ProviderDeezer, fmt.Sprintf("d-%d", i),
			fmt.Sprintf("Humble %d", i), fmt.Sprintf("Artist %d", i), nil)
		track.Album = fmt.Sprintf("Album %d", i)
		track.DeezerAlbumID = albumID
		organic = append(organic, track)
		tracksByAlbum[albumID] = []domain.SearchResult{
			trackResult(domain.ProviderDeezer, fmt.Sprintf("s-%d", i),
				fmt.Sprintf("Sibling %d", i), fmt.Sprintf("Artist %d", i), nil),
		}
	}
	return organic, tracksByAlbum
}

func libraryTitles(out *SearchOutput) []string {
	var titles []string
	for _, group := range out.Related {
		if group.Relationship != domain.RelationshipLibraryMatches {
			continue
		}
		for _, item := range group.Items {
			titles = append(titles, item.Title)
		}
	}
	return titles
}

func searchAs(t *testing.T, svc *Service, userId shared.UserId, raw string) *SearchOutput {
	t.Helper()
	out, err := svc.Execute(context.Background(), userId, newQuery(t, raw), false)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return out
}

func TestService_RepeatedSearch_ServesRelatedGroupsWithoutRefetching(t *testing.T) {
	organic, tracksByAlbum := relatedSearchFixture()
	querier := &countingRelationshipQuerier{}
	albumProvider := &countingAlbumTracksProvider{tracksByAlbum: tracksByAlbum}
	svc := NewService(
		[]ports.SearchProvider{&fakeProvider{name: domain.ProviderDeezer, results: organic}},
		NewCircuitBreaker(),
		WithResultCache(newFakeResultCache()),
		WithFindRelatedService(NewFindRelatedService(querier, albumProvider, nil)),
	)
	user := newUser()

	first := searchAs(t, svc, user, "humble")
	providerCalls, libraryCalls := albumProvider.calls.Load(), querier.calls.Load()
	if providerCalls != relatedTopN || libraryCalls != relatedTopN {
		t.Fatalf("first search: provider calls = %d, library queries = %d, want %d each",
			providerCalls, libraryCalls, relatedTopN)
	}

	second := searchAs(t, svc, user, "humble")

	if got := albumProvider.calls.Load() - providerCalls; got != 0 {
		t.Errorf("second search: provider calls = %d, want 0", got)
	}
	if got := querier.calls.Load() - libraryCalls; got != 0 {
		t.Errorf("second search: library queries = %d, want 0", got)
	}
	if len(second.Related) != len(first.Related) {
		t.Errorf("second search: related groups = %d, want %d (same slate, same groups)",
			len(second.Related), len(first.Related))
	}
}

func TestService_RepeatedSearch_KeepsRelatedLibraryMatchesPerUser(t *testing.T) {
	organic, tracksByAlbum := relatedSearchFixture()
	owner, other := newUser(), newUser()
	querier := &countingRelationshipQuerier{
		matchesByUser: map[shared.UserId][]ports.RelatedTrackMatch{
			owner: {{Title: "Owner Library Track", Artist: "Artist 0", Album: "Album 0"}},
			other: {{Title: "Other Library Track", Artist: "Artist 0", Album: "Album 0"}},
		},
	}
	svc := NewService(
		[]ports.SearchProvider{&fakeProvider{name: domain.ProviderDeezer, results: organic}},
		NewCircuitBreaker(),
		WithResultCache(newFakeResultCache()),
		WithFindRelatedService(NewFindRelatedService(querier, &countingAlbumTracksProvider{tracksByAlbum: tracksByAlbum}, nil)),
	)

	searchAs(t, svc, owner, "humble")
	otherOut := searchAs(t, svc, other, "humble")

	got := libraryTitles(otherOut)
	if len(got) == 0 {
		t.Fatal("second user got no library matches")
	}
	for _, title := range got {
		if title != "Other Library Track" {
			t.Errorf("second user was served %q, want only its own library matches", title)
		}
	}
}

type ownedTrackRow struct {
	owner shared.UserId
	match ports.RelatedTrackMatch
}

// ownershipQuerier holds library rows for several users, like the shared
// tracks table, and answers related-track lookups for the caller.
type ownershipQuerier struct {
	rows []ownedTrackRow
}

func (q *ownershipQuerier) FindRelatedByAlbum(_ context.Context, userId shared.UserId, album string, _ int) ([]ports.RelatedTrackMatch, error) {
	var out []ports.RelatedTrackMatch
	for _, r := range q.rows {
		if r.owner == userId && r.match.Album == album {
			out = append(out, r.match)
		}
	}
	return out, nil
}

// Regression for #570: a search must never surface another user's private
// library tracks in its related groups.
func TestSearch_RelatedLibraryMatches_NeverLeakAnotherUsersLibrary(t *testing.T) {
	userA := shared.NewUserId(uuid.New())
	userB := shared.NewUserId(uuid.New())
	const album = "Shared Album"

	querier := &ownershipQuerier{rows: []ownedTrackRow{
		{owner: userA, match: ports.RelatedTrackMatch{Title: "A Private Song", Artist: "Band", Album: album}},
		{owner: userB, match: ports.RelatedTrackMatch{Title: "B Own Song", Artist: "Band", Album: album}},
	}}

	organic := deezerTrack("Hit Single", "Band", 80)
	organic.Album = album
	p := &fakeProvider{name: domain.ProviderDeezer, results: []domain.SearchResult{organic}}
	svc := NewService([]ports.SearchProvider{p}, NewCircuitBreaker(),
		WithFindRelatedService(NewFindRelatedService(querier, nil, nil)))

	out, err := svc.Execute(context.Background(), userB, newQuery(t, "hit single"), false)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var sawOwn bool
	for _, g := range out.Related {
		for _, item := range g.Items {
			if item.Extras["source"] != "library" {
				continue
			}
			if item.Title == "A Private Song" {
				t.Errorf("user B's search leaked user A's library track %q in group %q", item.Title, g.Relationship)
			}
			if item.Title == "B Own Song" {
				sawOwn = true
			}
		}
	}
	if !sawOwn {
		t.Error("user B's own library track should still surface as a library match")
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

// Regression test for #568: a panic in a goroutine spawned around a provider
// or port call must be contained, not terminate the process.
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

// Regression test for #568: a panic in a goroutine spawned around a provider
// or port call must be contained, not terminate the process.
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
