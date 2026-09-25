package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/textnorm"
	"context"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

type fakeArtworkResolver struct {
	url   string
	calls int32
}

func (r *fakeArtworkResolver) ResolveTagged(_ context.Context, _ domain.ResultKind, _, _, _ string) (string, domain.ProviderKey, error) {
	atomic.AddInt32(&r.calls, 1)
	return r.url, "", nil
}

func (r *fakeArtworkResolver) ResolveWithIdentityTagged(_ context.Context, _ domain.ResultKind, _, _ string, _ ports.ArtworkIdentity) (string, domain.ProviderKey, error) {
	return "", "", nil
}

type fakeArtworkCache struct {
	mu    sync.Mutex
	store map[string]string
}

func (c *fakeArtworkCache) Get(_ context.Context, _ domain.ResultKind, title, _, _ string) (string, domain.ProviderKey, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	url, ok := c.store[title]
	return url, "", ok, nil
}

func (c *fakeArtworkCache) Set(_ context.Context, _ domain.ResultKind, title, _, _, url string, _ domain.ProviderKey, _ ports.ArtworkConfidence) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store[title] = url
	return nil
}

func TestService_EnrichesMissingArtwork(t *testing.T) {
	resolver := &fakeArtworkResolver{url: "https://art/cover.jpg"}
	p := &fakeProvider{name: domain.ProviderDeezer, results: []domain.SearchResult{deezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := NewService([]ports.SearchProvider{p}, NewCircuitBreaker(), WithArtworkResolver(resolver))

	out := runSearch(t, svc, "humble")

	if len(out.Results) != 1 {
		t.Fatalf("got %d results, want 1", len(out.Results))
	}
	if out.Results[0].ImageURL != "https://art/cover.jpg" {
		t.Errorf("artwork = %q, want resolved", out.Results[0].ImageURL)
	}
}

func TestService_SkipsEnrichWhenArtworkPresent(t *testing.T) {
	resolver := &fakeArtworkResolver{url: "https://art/new.jpg"}
	withArt := deezerTrack("Humble", "Kendrick Lamar", 80)
	withArt.ImageURL = "https://existing/art.jpg"
	p := &fakeProvider{name: domain.ProviderDeezer, results: []domain.SearchResult{withArt}}
	svc := NewService([]ports.SearchProvider{p}, NewCircuitBreaker(), WithArtworkResolver(resolver))

	out := runSearch(t, svc, "humble")

	if out.Results[0].ImageURL != "https://existing/art.jpg" {
		t.Errorf("artwork = %q, want the existing image kept", out.Results[0].ImageURL)
	}
	if n := atomic.LoadInt32(&resolver.calls); n != 0 {
		t.Errorf("resolver called %d times, want 0 (track already had art)", n)
	}
}

type capturingArtworkResolver struct {
	url     string
	mu      sync.Mutex
	gotMBID string
}

func (r *capturingArtworkResolver) ResolveTagged(_ context.Context, _ domain.ResultKind, _, _, mbid string) (string, domain.ProviderKey, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if mbid != "" {
		r.gotMBID = mbid
	}
	return r.url, "", nil
}

func (r *capturingArtworkResolver) ResolveWithIdentityTagged(_ context.Context, _ domain.ResultKind, _, _ string, _ ports.ArtworkIdentity) (string, domain.ProviderKey, error) {
	return "", "", nil
}

type fakeMBIDIndex struct {
	mbid    string
	lookups int32
}

func (f *fakeMBIDIndex) LookupMBID(_ context.Context, _ domain.ResultKind, _ string) (string, bool) {
	atomic.AddInt32(&f.lookups, 1)
	if f.mbid == "" {
		return "", false
	}
	return f.mbid, true
}

func (f *fakeMBIDIndex) RememberMBID(_ context.Context, _ domain.ResultKind, _, _ string) error {
	return nil
}

func TestService_MBIDIndexAttachesMBIDForArtwork(t *testing.T) {
	resolver := &capturingArtworkResolver{url: "https://caa/hd.jpg"}
	idx := &fakeMBIDIndex{mbid: "warm-mbid"}
	p := &fakeProvider{name: domain.ProviderDeezer, results: []domain.SearchResult{deezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := NewService(
		[]ports.SearchProvider{p},
		NewCircuitBreaker(),
		WithArtworkResolver(resolver),
		WithMBIDIndex(idx),
	)

	out := runSearch(t, svc, "humble")

	if out.Results[0].ImageURL != "https://caa/hd.jpg" {
		t.Errorf("artwork = %q, want resolved HD", out.Results[0].ImageURL)
	}
	if resolver.gotMBID != "warm-mbid" {
		t.Errorf("resolver got mbid %q, want the warmed MBID attached", resolver.gotMBID)
	}
	if atomic.LoadInt32(&idx.lookups) == 0 {
		t.Error("MBID index was never consulted")
	}
}

type fakeIdentityStore struct {
	mbid      string
	xref      map[string]string
	lookups   int32
	persisted int32
}

func (f *fakeIdentityStore) PersistBridges(_ context.Context, _ domain.ResultKind, _ string, _ map[string]string) error {
	atomic.AddInt32(&f.persisted, 1)
	return nil
}

func (f *fakeIdentityStore) LookupByProviderID(_ context.Context, _ domain.ResultKind, _ domain.ProviderKey, _ string) (string, map[string]string, bool) {
	atomic.AddInt32(&f.lookups, 1)
	if f.mbid == "" {
		return "", nil, false
	}
	return f.mbid, f.xref, true
}

func (f *fakeIdentityStore) Invalidate(_ context.Context, _ domain.ResultKind, _ domain.ProviderKey, _ string) error {
	return nil
}

func TestService_IdentityStoreResolvesArtworkWhenMBAbsent(t *testing.T) {
	resolver := &capturingArtworkResolver{url: "https://caa/right-face.jpg"}
	store := &fakeIdentityStore{mbid: "durable-mbid", xref: map[string]string{"discogs": "123"}}
	p := &fakeProvider{name: domain.ProviderDeezer, results: []domain.SearchResult{deezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := NewService(
		[]ports.SearchProvider{p},
		NewCircuitBreaker(),
		WithArtworkResolver(resolver),
		WithIdentityStore(store),
	)

	out := runSearch(t, svc, "humble")

	if atomic.LoadInt32(&store.lookups) == 0 {
		t.Error("identity store was never consulted")
	}
	if resolver.gotMBID != "durable-mbid" {
		t.Errorf("resolver got mbid %q, want the durable MBID attached from the store", resolver.gotMBID)
	}
	if out.Results[0].Xref["discogs"] != "123" {
		t.Errorf("xref = %v, want the bridged ids attached from the store", out.Results[0].Xref)
	}
}

type fakeIdentityAwareResolver struct{ url string }

func (r *fakeIdentityAwareResolver) ResolveTagged(_ context.Context, _ domain.ResultKind, _, _, _ string) (string, domain.ProviderKey, error) {
	return "", "", nil
}

func (r *fakeIdentityAwareResolver) ResolveWithIdentityTagged(_ context.Context, _ domain.ResultKind, _, _ string, id ports.ArtworkIdentity) (string, domain.ProviderKey, error) {
	if id.HasLinks() {
		return r.url, "", nil
	}
	return "", "", nil
}

func TestService_ArtworkPathIsDurableIdentityWhenStoreResolves(t *testing.T) {
	resolver := &fakeIdentityAwareResolver{url: "https://caa/right.jpg"}
	store := &fakeIdentityStore{mbid: "durable-mbid", xref: map[string]string{"discogs": "123"}}
	p := &fakeProvider{name: domain.ProviderDeezer, results: []domain.SearchResult{deezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := NewService(
		[]ports.SearchProvider{p},
		NewCircuitBreaker(),
		WithArtworkResolver(resolver),
		WithIdentityStore(store),
	)

	out := runSearch(t, svc, "humble")

	if out.Results[0].ImageURL != "https://caa/right.jpg" {
		t.Errorf("artwork = %q, want the identity-resolved image", out.Results[0].ImageURL)
	}
	if got, _ := out.Results[0].Extras["artwork_path"].(string); got != "durable-identity" {
		t.Errorf("artwork_path = %q, want durable-identity", got)
	}
}

// A provider outage must not be recorded as "this track has no art": the
// negative entry would short-circuit every later fill for the whole negative
// TTL, long after the providers came back.
func TestArtworkFiller_OutageLeavesTheCacheOpenForTheNextFill(t *testing.T) {
	track := domain.SearchResult{Kind: domain.ResultKindTrack, Title: "Humble", Subtitle: "Kendrick Lamar"}
	cache := &fakeArtworkCache{store: map[string]string{}}
	outage := &scriptedResolver{log: &stageLog{}, outage: errors.New("coverartarchive 503")}

	duringOutage := newArtworkFiller(outage, cache, nil, nil).fillOne(context.Background(), track)

	if cached, written := cache.store[track.Title]; written {
		t.Fatalf("the outage wrote a cache entry (%q); a negative entry blanks the art for hours", cached)
	}
	if path, _ := duringOutage.Extras["artwork_path"].(string); path != "degraded" {
		t.Errorf("artwork_path = %q, want degraded (an outage must not report as a clean miss)", path)
	}

	recovered := &scriptedResolver{log: &stageLog{}, nameURL: "https://caa/cover.jpg"}
	afterRecovery := newArtworkFiller(recovered, cache, nil, nil).fillOne(context.Background(), track)

	if afterRecovery.ImageURL != "https://caa/cover.jpg" {
		t.Errorf("ImageURL after recovery = %q, want the resolved cover", afterRecovery.ImageURL)
	}
}

func TestService_ArtworkCacheShortCircuits(t *testing.T) {
	resolver := &fakeArtworkResolver{url: "https://art/resolved.jpg"}
	cache := &fakeArtworkCache{store: map[string]string{"Humble": "https://art/cached.jpg"}}
	p := &fakeProvider{name: domain.ProviderDeezer, results: []domain.SearchResult{deezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := NewService(
		[]ports.SearchProvider{p},
		NewCircuitBreaker(),
		WithArtworkResolver(resolver),
		WithArtworkCache(cache),
	)

	out := runSearch(t, svc, "humble")

	if out.Results[0].ImageURL != "https://art/cached.jpg" {
		t.Errorf("artwork = %q, want the cached image", out.Results[0].ImageURL)
	}
	if n := atomic.LoadInt32(&resolver.calls); n != 0 {
		t.Errorf("resolver called %d times, want 0 (cache hit)", n)
	}
}

// countingIdentityStore counts every store call so the fill's round-trip cost
// is measurable. It knows an identity for every even external id.
type countingIdentityStore struct {
	mu          sync.Mutex
	singleCalls int
}

func (s *countingIdentityStore) PersistBridges(context.Context, domain.ResultKind, string, map[string]string) error {
	return nil
}

func (s *countingIdentityStore) LookupByProviderID(_ context.Context, kind domain.ResultKind, provider domain.ProviderKey, externalID string) (string, map[string]string, bool) {
	s.mu.Lock()
	s.singleCalls++
	s.mu.Unlock()
	hit, ok := knownIdentity(ports.IdentityRef{Kind: kind, Provider: provider, ExternalID: externalID})
	return hit.MBID, hit.Xref, ok
}

func (s *countingIdentityStore) Invalidate(context.Context, domain.ResultKind, domain.ProviderKey, string) error {
	return nil
}

func knownIdentity(ref ports.IdentityRef) (ports.IdentityHit, bool) {
	n, err := strconv.Atoi(ref.ExternalID)
	if err != nil || n%2 != 0 {
		return ports.IdentityHit{}, false
	}
	return ports.IdentityHit{MBID: "mbid-" + ref.ExternalID, Xref: map[string]string{"discogs": "d" + ref.ExternalID}}, true
}

// batchingIdentityStore also offers the one-round-trip batch lookup.
type batchingIdentityStore struct {
	countingIdentityStore
	batchCalls int
	batchRefs  []ports.IdentityRef
}

func (s *batchingIdentityStore) LookupByProviderIDs(_ context.Context, refs []ports.IdentityRef) map[ports.IdentityRef]ports.IdentityHit {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.batchCalls++
	s.batchRefs = append(s.batchRefs, refs...)
	hits := map[ports.IdentityRef]ports.IdentityHit{}
	for _, ref := range refs {
		if hit, ok := knownIdentity(ref); ok {
			hits[ref] = hit
		}
	}
	return hits
}

func fiftyArtlessResults() []domain.SearchResult {
	results := make([]domain.SearchResult, artworkFillLimit)
	for i := range results {
		id := strconv.Itoa(i)
		results[i] = domain.NewProviderResult(domain.ResultKindTrack, "Song "+id, "Artist", "",
			domain.SourceRef{Provider: domain.ProviderDeezer, ExternalID: id}, nil)
	}
	return results
}

func assertDurableXrefApplied(t *testing.T, got []domain.SearchResult) {
	t.Helper()
	for i, r := range got {
		want := ""
		if i%2 == 0 {
			want = "d" + strconv.Itoa(i)
		}
		if r.Xref["discogs"] != want {
			t.Errorf("result %d xref = %v, want discogs=%q", i, r.Xref, want)
		}
	}
}

func TestArtworkFiller_BatchesDurableIdentityLookups(t *testing.T) {
	store := &batchingIdentityStore{}
	f := newArtworkFiller(&fakeArtworkResolver{}, nil, store, nil)

	got := f.fill(context.Background(), fiftyArtlessResults())

	if store.batchCalls != 1 {
		t.Errorf("batch lookups = %d, want 1 for a %d-result slate", store.batchCalls, artworkFillLimit)
	}
	if store.singleCalls != 0 {
		t.Errorf("per-result lookups = %d, want 0 when the store batches", store.singleCalls)
	}
	if len(store.batchRefs) != artworkFillLimit {
		t.Errorf("batched refs = %d, want %d", len(store.batchRefs), artworkFillLimit)
	}
	assertDurableXrefApplied(t, got)
}

func TestArtworkFiller_BatchSkipsResultsThatNeedNoDurableLookup(t *testing.T) {
	store := &batchingIdentityStore{}
	f := newArtworkFiller(&fakeArtworkResolver{}, nil, store, nil)
	results := fiftyArtlessResults()[:3]
	results[0].ImageURL = "https://provider/art.jpg"
	results[1].Xref = map[string]string{"spotify": "s1"}

	f.fill(context.Background(), results)

	want := []ports.IdentityRef{{Kind: domain.ResultKindTrack, Provider: domain.ProviderDeezer.Key(), ExternalID: "2"}}
	if len(store.batchRefs) != 1 || store.batchRefs[0] != want[0] {
		t.Errorf("batched refs = %v, want only %v", store.batchRefs, want)
	}
	if store.singleCalls != 0 {
		t.Errorf("per-result lookups = %d, want 0", store.singleCalls)
	}
}

func TestArtworkFiller_FallsBackToPerResultLookupWithoutBatch(t *testing.T) {
	store := &countingIdentityStore{}
	f := newArtworkFiller(&fakeArtworkResolver{}, nil, store, nil)

	got := f.fill(context.Background(), fiftyArtlessResults())

	if store.singleCalls != artworkFillLimit {
		t.Errorf("per-result lookups = %d, want %d for a non-batching store", store.singleCalls, artworkFillLimit)
	}
	assertDurableXrefApplied(t, got)
}

// stageLog records every port call the artwork cascade makes, in order, so the
// table test below pins stage order and fallthrough, not just the final label.
type stageLog struct{ calls []string }

func (l *stageLog) add(s string) { l.calls = append(l.calls, s) }

type scriptedIdentityStore struct {
	log  *stageLog
	mbid string
	xref map[string]string
	ok   bool
}

func (s *scriptedIdentityStore) PersistBridges(context.Context, domain.ResultKind, string, map[string]string) error {
	return nil
}

func (s *scriptedIdentityStore) LookupByProviderID(_ context.Context, _ domain.ResultKind, provider domain.ProviderKey, externalID string) (string, map[string]string, bool) {
	s.log.add("durable:" + provider.String() + "/" + externalID)
	return s.mbid, s.xref, s.ok
}

func (s *scriptedIdentityStore) Invalidate(context.Context, domain.ResultKind, domain.ProviderKey, string) error {
	return nil
}

type scriptedMBIDIndex struct {
	log  *stageLog
	mbid string
	ok   bool
}

func (m *scriptedMBIDIndex) LookupMBID(_ context.Context, _ domain.ResultKind, key string) (string, bool) {
	m.log.add("index:" + key)
	return m.mbid, m.ok
}

func (m *scriptedMBIDIndex) RememberMBID(context.Context, domain.ResultKind, string, string) error {
	return nil
}

type scriptedArtworkCache struct {
	log    *stageLog
	url    string
	source string
	found  bool
}

func (c *scriptedArtworkCache) Get(_ context.Context, _ domain.ResultKind, _, _, mbid string) (string, domain.ProviderKey, bool, error) {
	c.log.add("cache.get:" + mbid)
	return c.url, domain.ProviderKey(c.source), c.found, nil
}

func (c *scriptedArtworkCache) Set(_ context.Context, _ domain.ResultKind, _, _, mbid, url string, source domain.ProviderKey, confidence ports.ArtworkConfidence) error {
	c.log.add("cache.set:" + mbid + "|" + url + "|" + source.String() + "|" + artworkPathFor(url, confidence, false))
	return nil
}

type scriptedResolver struct {
	log         *stageLog
	identityURL string
	nameURL     string
	outage      error // every leg reports this instead of a clean miss
}

func (r *scriptedResolver) ResolveWithIdentityTagged(_ context.Context, _ domain.ResultKind, _, _ string, id ports.ArtworkIdentity) (string, domain.ProviderKey, error) {
	r.log.add("resolve.identity:" + id.MBID + "|" + strings.Join(sortedXrefKeys(id.ExternalIDs), ","))
	if r.identityURL == "" {
		return "", "", r.outage
	}
	return r.identityURL, "id-src", nil
}

func (r *scriptedResolver) ResolveTagged(_ context.Context, _ domain.ResultKind, _, _, mbid string) (string, domain.ProviderKey, error) {
	r.log.add("resolve.name:" + mbid)
	if r.nameURL == "" {
		return "", "", r.outage
	}
	return r.nameURL, "name-src", nil
}

func sortedXrefKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

func TestArtworkFiller_FillOneStageCascade(t *testing.T) {
	deezerSrc := []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "42"}}
	key := textnorm.NameKey("Humble", "Kendrick Lamar")

	type stages struct {
		durable *scriptedIdentityStore
		index   *scriptedMBIDIndex
		cache   *scriptedArtworkCache
		live    scriptedResolver
	}
	cases := []struct {
		name       string
		in         domain.SearchResult
		st         stages
		noDurable  bool
		noIndex    bool
		noCache    bool
		wantPath   string
		wantURL    string
		wantSource string
		wantXref   map[string]string
		wantCalls  []string
	}{
		{
			name:       "provider art present skips every stage and defaults source",
			in:         domain.SearchResult{Kind: domain.ResultKindTrack, Title: "Humble", Subtitle: "Kendrick Lamar", ImageURL: "https://p/a.jpg", Sources: deezerSrc},
			st:         stages{durable: &scriptedIdentityStore{ok: true, mbid: "d"}, cache: &scriptedArtworkCache{found: true, url: "https://c"}},
			wantPath:   "provider",
			wantURL:    "https://p/a.jpg",
			wantSource: "deezer",
			wantCalls:  nil,
		},
		{
			name:       "provider art keeps an explicit artwork source",
			in:         domain.SearchResult{Kind: domain.ResultKindTrack, Title: "Humble", ImageURL: "https://p/a.jpg", ArtworkSource: "itunes", Sources: deezerSrc},
			wantPath:   "provider",
			wantURL:    "https://p/a.jpg",
			wantSource: "itunes",
		},
		{
			name:      "empty-art hash placeholder counts as missing",
			in:        domain.SearchResult{Kind: domain.ResultKindTrack, Title: "Humble", Subtitle: "Kendrick Lamar", ImageURL: "https://p/" + emptyArtHash + ".jpg", Sources: deezerSrc},
			st:        stages{live: scriptedResolver{nameURL: "https://n.jpg"}},
			noDurable: true, noIndex: true, noCache: true,
			wantPath:   "name",
			wantURL:    "https://n.jpg",
			wantSource: "name-src",
			wantCalls:  []string{"resolve.name:"},
		},
		{
			name: "durable hit supplies mbid and xref, skips index, cache hit wins",
			in:   domain.SearchResult{Kind: domain.ResultKindTrack, Title: "Humble", Subtitle: "Kendrick Lamar", Sources: deezerSrc},
			st: stages{
				durable: &scriptedIdentityStore{ok: true, mbid: "dur", xref: map[string]string{"discogs": "1"}},
				index:   &scriptedMBIDIndex{ok: true, mbid: "idx"},
				cache:   &scriptedArtworkCache{found: true, url: "https://c.jpg", source: "caa"},
			},
			wantPath:   "cache",
			wantURL:    "https://c.jpg",
			wantSource: "caa",
			wantXref:   map[string]string{"discogs": "1"},
			wantCalls:  []string{"durable:deezer/42", "cache.get:dur"},
		},
		{
			name: "result mbid beats durable mbid but durable still flags the path",
			in:   domain.SearchResult{Kind: domain.ResultKindTrack, Title: "Humble", Subtitle: "Kendrick Lamar", MBID: "own", Sources: deezerSrc},
			st: stages{
				durable: &scriptedIdentityStore{ok: true, mbid: "dur"},
				index:   &scriptedMBIDIndex{ok: true, mbid: "idx"},
				cache:   &scriptedArtworkCache{},
				live:    scriptedResolver{identityURL: "https://id.jpg"},
			},
			wantPath:   "durable-identity",
			wantURL:    "https://id.jpg",
			wantSource: "id-src",
			wantCalls:  []string{"durable:deezer/42", "cache.get:own", "resolve.identity:own|", "cache.set:own|https://id.jpg|id-src|identity"},
		},
		{
			name: "durable hit with empty mbid falls through to index and keeps no xref",
			in:   domain.SearchResult{Kind: domain.ResultKindTrack, Title: "Humble", Subtitle: "Kendrick Lamar", Sources: deezerSrc},
			st: stages{
				durable: &scriptedIdentityStore{ok: true},
				index:   &scriptedMBIDIndex{ok: true, mbid: "idx"},
				cache:   &scriptedArtworkCache{},
				live:    scriptedResolver{identityURL: "https://id.jpg"},
			},
			wantPath:   "durable-identity",
			wantURL:    "https://id.jpg",
			wantSource: "id-src",
			wantCalls:  []string{"durable:deezer/42", "index:" + key, "cache.get:idx", "resolve.identity:idx|", "cache.set:idx|https://id.jpg|id-src|identity"},
		},
		{
			name: "existing xref skips the durable store",
			in:   domain.SearchResult{Kind: domain.ResultKindTrack, Title: "Humble", Subtitle: "Kendrick Lamar", Sources: deezerSrc, Xref: map[string]string{"spotify": "s"}},
			st: stages{
				durable: &scriptedIdentityStore{ok: true, mbid: "dur"},
				index:   &scriptedMBIDIndex{},
				cache:   &scriptedArtworkCache{},
				live:    scriptedResolver{identityURL: "https://id.jpg"},
			},
			wantPath:   "identity",
			wantURL:    "https://id.jpg",
			wantSource: "id-src",
			wantXref:   map[string]string{"spotify": "s"},
			wantCalls:  []string{"index:" + key, "cache.get:", "resolve.identity:|spotify", "cache.set:|https://id.jpg|id-src|identity"},
		},
		{
			name: "no sources skips the durable store",
			in:   domain.SearchResult{Kind: domain.ResultKindTrack, Title: "Humble", Subtitle: "Kendrick Lamar"},
			st: stages{
				durable: &scriptedIdentityStore{ok: true, mbid: "dur"},
				index:   &scriptedMBIDIndex{ok: true, mbid: "idx"},
				live:    scriptedResolver{nameURL: "https://n.jpg"},
			},
			noCache:    true,
			wantPath:   "name",
			wantURL:    "https://n.jpg",
			wantSource: "name-src",
			wantCalls:  []string{"index:" + key, "resolve.identity:idx|", "resolve.name:idx"},
		},
		{
			name: "durable miss then index miss leaves mbid empty",
			in:   domain.SearchResult{Kind: domain.ResultKindTrack, Title: "Humble", Subtitle: "Kendrick Lamar", Sources: deezerSrc},
			st: stages{
				durable: &scriptedIdentityStore{},
				index:   &scriptedMBIDIndex{},
				cache:   &scriptedArtworkCache{},
				live:    scriptedResolver{nameURL: "https://n.jpg"},
			},
			wantPath:   "name",
			wantURL:    "https://n.jpg",
			wantSource: "name-src",
			wantCalls:  []string{"durable:deezer/42", "index:" + key, "cache.get:", "resolve.name:", "cache.set:|https://n.jpg|name-src|name"},
		},
		{
			name:      "cached miss settles a non-artist as none",
			in:        domain.SearchResult{Kind: domain.ResultKindAlbum, Title: "Humble", Subtitle: "Kendrick Lamar"},
			st:        stages{cache: &scriptedArtworkCache{found: true}, live: scriptedResolver{nameURL: "https://n.jpg"}},
			noDurable: true, noIndex: true,
			wantPath:  "none",
			wantCalls: []string{"cache.get:"},
		},
		{
			name:      "cached empty-art hash settles a non-artist as none",
			in:        domain.SearchResult{Kind: domain.ResultKindTrack, Title: "Humble", Subtitle: "Kendrick Lamar"},
			st:        stages{cache: &scriptedArtworkCache{found: true, url: "https://c/" + emptyArtHash, source: "caa"}, live: scriptedResolver{nameURL: "https://n.jpg"}},
			noDurable: true, noIndex: true,
			wantPath:  "none",
			wantCalls: []string{"cache.get:"},
		},
		{
			name:      "cached miss for an artist falls through to the live resolver",
			in:        domain.SearchResult{Kind: domain.ResultKindArtist, Title: "Kendrick Lamar"},
			st:        stages{cache: &scriptedArtworkCache{found: true}, live: scriptedResolver{nameURL: "https://n.jpg"}},
			noDurable: true, noIndex: true,
			wantPath:   "name",
			wantURL:    "https://n.jpg",
			wantSource: "name-src",
			wantCalls:  []string{"cache.get:", "resolve.name:", "cache.set:|https://n.jpg|name-src|name"},
		},
		{
			name:      "live resolver miss stamps none and caches the miss",
			in:        domain.SearchResult{Kind: domain.ResultKindTrack, Title: "Humble", Subtitle: "Kendrick Lamar", MBID: "own"},
			st:        stages{cache: &scriptedArtworkCache{}},
			noDurable: true, noIndex: true,
			wantPath:  "none",
			wantCalls: []string{"cache.get:own", "resolve.identity:own|", "resolve.name:own", "cache.set:own|||none"},
		},
		{
			name:      "a miss from failing resolvers is stamped degraded and never cached",
			in:        domain.SearchResult{Kind: domain.ResultKindTrack, Title: "Humble", Subtitle: "Kendrick Lamar", MBID: "own"},
			st:        stages{cache: &scriptedArtworkCache{}, live: scriptedResolver{outage: errors.New("coverartarchive 503")}},
			noDurable: true, noIndex: true,
			wantPath:  "degraded",
			wantCalls: []string{"cache.get:own", "resolve.identity:own|", "resolve.name:own"},
		},
		{
			name:      "identity miss falls back to name lookup",
			in:        domain.SearchResult{Kind: domain.ResultKindTrack, Title: "Humble", Subtitle: "Kendrick Lamar", MBID: "own"},
			st:        stages{live: scriptedResolver{nameURL: "https://n.jpg"}},
			noDurable: true, noIndex: true, noCache: true,
			wantPath:   "name",
			wantURL:    "https://n.jpg",
			wantSource: "name-src",
			wantCalls:  []string{"resolve.identity:own|", "resolve.name:own"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			log := &stageLog{}
			tc.st.live.log = log
			if tc.st.durable == nil {
				tc.st.durable = &scriptedIdentityStore{}
			}
			if tc.st.index == nil {
				tc.st.index = &scriptedMBIDIndex{}
			}
			if tc.st.cache == nil {
				tc.st.cache = &scriptedArtworkCache{}
			}
			tc.st.durable.log, tc.st.index.log, tc.st.cache.log = log, log, log

			var store ports.IdentityStore = tc.st.durable
			var index ports.MBIDIndex = tc.st.index
			var cache ports.ArtworkCache = tc.st.cache
			if tc.noDurable {
				store = nil
			}
			if tc.noIndex {
				index = nil
			}
			if tc.noCache {
				cache = nil
			}
			f := newArtworkFiller(&tc.st.live, cache, store, index)

			got := f.fillOne(context.Background(), tc.in)

			if path, _ := got.Extras["artwork_path"].(string); path != tc.wantPath {
				t.Errorf("artwork_path = %q, want %q", path, tc.wantPath)
			}
			wantURL := tc.wantURL
			if wantURL == "" {
				wantURL = tc.in.ImageURL
			}
			if got.ImageURL != wantURL {
				t.Errorf("ImageURL = %q, want %q", got.ImageURL, wantURL)
			}
			if got.ArtworkSource != tc.wantSource {
				t.Errorf("ArtworkSource = %q, want %q", got.ArtworkSource, tc.wantSource)
			}
			if tc.wantXref != nil && !reflect.DeepEqual(got.Xref, tc.wantXref) {
				t.Errorf("Xref = %v, want %v", got.Xref, tc.wantXref)
			}
			if !reflect.DeepEqual(log.calls, tc.wantCalls) {
				t.Errorf("stage calls =\n  %q\nwant\n  %q", log.calls, tc.wantCalls)
			}
		})
	}
}
