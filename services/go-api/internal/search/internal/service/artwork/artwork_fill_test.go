package artwork_test

import (
	"altune/go-api/internal/search/internal/core/ports"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/service/artwork"
	"altune/go-api/internal/search/searchtest"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/textnorm"
	"context"
	"errors"
	"log/slog"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
)

const (
	slateLimit         = 50
	placeholderArtHash = "d41d8cd98f00b204e9800998ecf8427e"
)

func fillAndSettle(t *testing.T, bg *background.Tracker, f *artwork.ArtworkFiller, results ...domain.SearchResult) []domain.SearchResult {
	t.Helper()
	got := f.Fill(context.Background(), results)
	bg.Wait()
	return got
}

func newFiller(t *testing.T, bg *background.Tracker, resolver ports.TaggingArtworkLookup, cache ports.ArtworkCache) *artwork.ArtworkFiller {
	t.Helper()
	f, err := artwork.NewArtworkFiller(resolver, cache, artwork.NoIdentities{}, artwork.NoMBIDIndex{}, bg)
	if err != nil {
		t.Fatalf("NewArtworkFiller: %v", err)
	}
	return f
}

func newPendingFiller(t *testing.T, bg *background.Tracker, store ports.IdentityStore) *artwork.ArtworkFiller {
	t.Helper()
	f, err := artwork.NewPendingFiller(store, bg)
	if err != nil {
		t.Fatalf("NewPendingFiller: %v", err)
	}
	return f
}

func TestArtworkFiller_RefusesEveryMissingCollaborator(t *testing.T) {
	bg := searchtest.NewTracker(t)
	var (
		lookup     ports.TaggingArtworkLookup = artwork.NoLookup{}
		cache      ports.ArtworkCache         = artwork.NoCache{}
		identities ports.IdentityStore        = artwork.NoIdentities{}
		index      ports.MBIDIndex            = artwork.NoMBIDIndex{}
	)
	for name, build := range map[string]func() error{
		"resolver":      func() error { _, err := artwork.NewArtworkFiller(nil, cache, identities, index, bg); return err },
		"cache":         func() error { _, err := artwork.NewArtworkFiller(lookup, nil, identities, index, bg); return err },
		"identityStore": func() error { _, err := artwork.NewArtworkFiller(lookup, cache, nil, index, bg); return err },
		"mbidIndex":     func() error { _, err := artwork.NewArtworkFiller(lookup, cache, identities, nil, bg); return err },
		"tracker":       func() error { _, err := artwork.NewArtworkFiller(lookup, cache, identities, index, nil); return err },
		"pending store": func() error { _, err := artwork.NewPendingFiller(nil, bg); return err },
	} {
		if err := build(); !errors.Is(err, artwork.ErrCollaboratorRequired) {
			t.Errorf("without %s: err = %v, want ErrCollaboratorRequired", name, err)
		}
	}
}

func TestNoFill_ReturnsTheResultsUntouched(t *testing.T) {
	in := []domain.SearchResult{{Kind: domain.ResultKindTrack, Title: "Humble"}}
	got := artwork.NoFill{}.Fill(context.Background(), in)
	if len(got) != 1 || got[0].ArtworkPath != "" {
		t.Fatalf("NoFill = %+v, want the input untouched", got)
	}
}

func TestPendingFiller_MarksArtworkPendingAndNeverResolvesLive(t *testing.T) {
	track := domain.SearchResult{Kind: domain.ResultKindTrack, Title: "Humble", Subtitle: "Kendrick Lamar"}
	liveRan := func(build func(*background.Tracker) *artwork.ArtworkFiller) (domain.ArtworkPath, bool) {
		logs := logging.CaptureText(t, slog.LevelDebug)
		bg := searchtest.NewTracker(t)
		got := fillAndSettle(t, bg, build(bg), track)
		return got[0].ArtworkPath, strings.Contains(logs.String(), "artwork.enriched")
	}

	path, ran := liveRan(func(bg *background.Tracker) *artwork.ArtworkFiller {
		return newPendingFiller(t, bg, artwork.NoIdentities{})
	})
	if path != domain.ArtworkPathPending || ran {
		t.Errorf("pending filler: path = %q, live resolution ran = %v, want pending and no live resolution", path, ran)
	}

	_, ran = liveRan(func(bg *background.Tracker) *artwork.ArtworkFiller {
		return newFiller(t, bg, artwork.NoLookup{}, artwork.NoCache{})
	})
	if !ran {
		t.Error("control: the live filler never reached live resolution, so the pending assertion above proves nothing")
	}
}

func TestArtworkFiller_OutageLeavesTheCacheOpenForTheNextFill(t *testing.T) {
	track := domain.SearchResult{Kind: domain.ResultKindTrack, Title: "Humble", Subtitle: "Kendrick Lamar"}
	cache := &searchtest.FakeArtworkCache{Store: map[string]string{}}
	outage := &scriptedResolver{log: &stageLog{}, outage: errors.New("coverartarchive 503")}

	bg := searchtest.NewTracker(t)
	fillAndSettle(t, bg, newFiller(t, bg, outage, cache), track)

	if want := []string{"resolve.name:"}; !reflect.DeepEqual(outage.log.calls, want) {
		t.Fatalf("resolver calls during the outage = %q, want %q", outage.log.calls, want)
	}
	if cached, written := cache.Store[track.Title]; written {
		t.Fatalf("the outage wrote a cache entry (%q); a negative entry blanks the art for hours", cached)
	}

	recovered := &scriptedResolver{log: &stageLog{}, nameURL: "https://caa/cover.jpg"}
	fillAndSettle(t, bg, newFiller(t, bg, recovered, cache), track)
	afterRecovery := fillAndSettle(t, bg, newFiller(t, bg, recovered, cache), track)

	if afterRecovery[0].ImageURL != "https://caa/cover.jpg" || afterRecovery[0].ArtworkPath != domain.ArtworkPathCache {
		t.Errorf("after recovery = (%q, %q), want the resolved cover served from the cache", afterRecovery[0].ImageURL, afterRecovery[0].ArtworkPath)
	}
}

type failingArtworkCache struct{ scriptedArtworkCache }

func (failingArtworkCache) Set(context.Context, domain.ResultKind, string, string, string, string, domain.ProviderKey, ports.ArtworkConfidence) error {
	return errors.New("redis down")
}

func TestArtworkFiller_AFailedCacheWriteIsLogged(t *testing.T) {
	logs := logging.CaptureText(t, slog.LevelInfo)
	track := domain.SearchResult{Kind: domain.ResultKindTrack, Title: "Humble", Subtitle: "Kendrick Lamar"}
	cache := &failingArtworkCache{scriptedArtworkCache{log: &stageLog{}}}
	resolver := &scriptedResolver{log: &stageLog{}, nameURL: "https://caa/cover.jpg"}

	bg := searchtest.NewTracker(t)
	fillAndSettle(t, bg, newFiller(t, bg, resolver, cache), track)

	if want := []string{"resolve.name:"}; !reflect.DeepEqual(resolver.log.calls, want) {
		t.Fatalf("resolver calls = %q, want %q", resolver.log.calls, want)
	}
	if !strings.Contains(logs.String(), "artwork.cache_set_failed") {
		t.Errorf("failed cache write not logged: %q", logs.String())
	}
}

type countingIdentityStore struct {
	mu          sync.Mutex
	singleCalls int
}

func (s *countingIdentityStore) LookupByProviderID(_ context.Context, kind domain.ResultKind, provider domain.ProviderKey, externalID string) (string, map[domain.ProviderKey]string, bool) {
	s.mu.Lock()
	s.singleCalls++
	s.mu.Unlock()
	hit, ok := knownIdentity(ports.IdentityRef{Kind: kind, Provider: provider, ExternalID: externalID})
	return hit.MBID, hit.Xref, ok
}

func knownIdentity(ref ports.IdentityRef) (ports.IdentityHit, bool) {
	n, err := strconv.Atoi(ref.ExternalID)
	if err != nil || n%2 != 0 {
		return ports.IdentityHit{}, false
	}
	return ports.IdentityHit{MBID: "mbid-" + ref.ExternalID, Xref: map[domain.ProviderKey]string{"discogs": "d" + ref.ExternalID}}, true
}

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
	results := make([]domain.SearchResult, slateLimit)
	for i := range results {
		id := strconv.Itoa(i)
		results[i] = domain.NewProviderResult(domain.ResultKindTrack, "Song "+id, "Artist", "",
			domain.ProviderLink{Provider: domain.ProviderDeezer, ExternalID: id}, nil)
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
	bg := searchtest.NewTracker(t)
	f := newPendingFiller(t, bg, store)

	got := f.Fill(context.Background(), fiftyArtlessResults())

	if store.batchCalls != 1 {
		t.Errorf("batch lookups = %d, want 1 for a %d-result slate", store.batchCalls, slateLimit)
	}
	if store.singleCalls != 0 {
		t.Errorf("per-result lookups = %d, want 0 when the store batches", store.singleCalls)
	}
	if len(store.batchRefs) != slateLimit {
		t.Errorf("batched refs = %d, want %d", len(store.batchRefs), slateLimit)
	}
	assertDurableXrefApplied(t, got)
}

func TestArtworkFiller_BatchSkipsResultsThatNeedNoDurableLookup(t *testing.T) {
	store := &batchingIdentityStore{}
	bg := searchtest.NewTracker(t)
	f := newPendingFiller(t, bg, store)
	results := fiftyArtlessResults()[:3]
	results[0].ImageURL = "https://provider/art.jpg"
	results[1].Xref = map[domain.ProviderKey]string{"spotify": "s1"}

	f.Fill(context.Background(), results)

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
	bg := searchtest.NewTracker(t)
	f := newPendingFiller(t, bg, store)

	got := f.Fill(context.Background(), fiftyArtlessResults())

	if store.singleCalls != slateLimit {
		t.Errorf("per-result lookups = %d, want %d for a non-batching store", store.singleCalls, slateLimit)
	}
	assertDurableXrefApplied(t, got)
}

type stageLog struct{ calls []string }

func (l *stageLog) add(s string) { l.calls = append(l.calls, s) }

type scriptedIdentityStore struct {
	log  *stageLog
	mbid string
	xref map[domain.ProviderKey]string
	ok   bool
}

func (s *scriptedIdentityStore) LookupByProviderID(_ context.Context, _ domain.ResultKind, provider domain.ProviderKey, externalID string) (string, map[domain.ProviderKey]string, bool) {
	s.log.add("durable:" + provider.String() + "/" + externalID)
	return s.mbid, s.xref, s.ok
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
	c.log.add("cache.set:" + mbid + "|" + url + "|" + source.String() + "|" + confidenceName(confidence))
	return nil
}

func confidenceName(c ports.ArtworkConfidence) string {
	switch c {
	case ports.ArtworkConfidenceIdentity:
		return "identity"
	case ports.ArtworkConfidenceName:
		return "name"
	case ports.ArtworkConfidenceNone:
		return "none"
	}
	return "unknown confidence " + strconv.Itoa(int(c))
}

type scriptedResolver struct {
	log         *stageLog
	identityURL string
	nameURL     string
	outage      error
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

func sortedXrefKeys(m map[domain.ProviderKey]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, string(k))
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

func TestArtworkFiller_FillStageCascade(t *testing.T) {
	deezerSrc := []domain.ProviderLink{{Provider: domain.ProviderDeezer, ExternalID: "42"}}
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
		wantSource domain.ProviderKey
		wantXref   map[domain.ProviderKey]string
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
			in:        domain.SearchResult{Kind: domain.ResultKindTrack, Title: "Humble", Subtitle: "Kendrick Lamar", ImageURL: "https://p/" + placeholderArtHash + ".jpg", Sources: deezerSrc},
			st:        stages{cache: &scriptedArtworkCache{}, live: scriptedResolver{nameURL: "https://n.jpg"}},
			noDurable: true, noIndex: true,
			wantPath:  "pending",
			wantCalls: []string{"cache.get:", "resolve.name:", "cache.set:|https://n.jpg|name-src|name"},
		},
		{
			name: "durable hit supplies mbid and xref, skips index, cache hit wins",
			in:   domain.SearchResult{Kind: domain.ResultKindTrack, Title: "Humble", Subtitle: "Kendrick Lamar", Sources: deezerSrc},
			st: stages{
				durable: &scriptedIdentityStore{ok: true, mbid: "dur", xref: map[domain.ProviderKey]string{"discogs": "1"}},
				index:   &scriptedMBIDIndex{ok: true, mbid: "idx"},
				cache:   &scriptedArtworkCache{found: true, url: "https://c.jpg", source: "caa"},
			},
			wantPath:   "cache",
			wantURL:    "https://c.jpg",
			wantSource: "caa",
			wantXref:   map[domain.ProviderKey]string{"discogs": "1"},
			wantCalls:  []string{"durable:deezer/42", "cache.get:dur"},
		},
		{
			name: "result mbid beats durable mbid",
			in:   domain.SearchResult{Kind: domain.ResultKindTrack, Title: "Humble", Subtitle: "Kendrick Lamar", RecordingMBID: "own", Sources: deezerSrc},
			st: stages{
				durable: &scriptedIdentityStore{ok: true, mbid: "dur"},
				index:   &scriptedMBIDIndex{ok: true, mbid: "idx"},
				cache:   &scriptedArtworkCache{},
				live:    scriptedResolver{identityURL: "https://id.jpg"},
			},
			wantPath:  "pending",
			wantCalls: []string{"durable:deezer/42", "cache.get:own", "resolve.identity:own|", "cache.set:own|https://id.jpg|id-src|identity"},
		},
		{
			name: "durable hit with empty mbid falls through to index",
			in:   domain.SearchResult{Kind: domain.ResultKindTrack, Title: "Humble", Subtitle: "Kendrick Lamar", Sources: deezerSrc},
			st: stages{
				durable: &scriptedIdentityStore{ok: true},
				index:   &scriptedMBIDIndex{ok: true, mbid: "idx"},
				cache:   &scriptedArtworkCache{},
				live:    scriptedResolver{identityURL: "https://id.jpg"},
			},
			wantPath:  "pending",
			wantCalls: []string{"durable:deezer/42", "index:" + key, "cache.get:idx", "resolve.identity:idx|", "cache.set:idx|https://id.jpg|id-src|identity"},
		},
		{
			name: "existing xref skips the durable store",
			in:   domain.SearchResult{Kind: domain.ResultKindTrack, Title: "Humble", Subtitle: "Kendrick Lamar", Sources: deezerSrc, Xref: map[domain.ProviderKey]string{"spotify": "s"}},
			st: stages{
				durable: &scriptedIdentityStore{ok: true, mbid: "dur"},
				index:   &scriptedMBIDIndex{},
				cache:   &scriptedArtworkCache{},
				live:    scriptedResolver{identityURL: "https://id.jpg"},
			},
			wantPath:  "pending",
			wantXref:  map[domain.ProviderKey]string{"spotify": "s"},
			wantCalls: []string{"index:" + key, "cache.get:", "resolve.identity:|spotify", "cache.set:|https://id.jpg|id-src|identity"},
		},
		{
			name: "no sources skips the durable store",
			in:   domain.SearchResult{Kind: domain.ResultKindTrack, Title: "Humble", Subtitle: "Kendrick Lamar"},
			st: stages{
				durable: &scriptedIdentityStore{ok: true, mbid: "dur"},
				index:   &scriptedMBIDIndex{ok: true, mbid: "idx"},
				cache:   &scriptedArtworkCache{},
				live:    scriptedResolver{nameURL: "https://n.jpg"},
			},
			wantPath:  "pending",
			wantCalls: []string{"index:" + key, "cache.get:idx", "resolve.identity:idx|", "resolve.name:idx", "cache.set:idx|https://n.jpg|name-src|name"},
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
			wantPath:  "pending",
			wantCalls: []string{"durable:deezer/42", "index:" + key, "cache.get:", "resolve.name:", "cache.set:|https://n.jpg|name-src|name"},
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
			st:        stages{cache: &scriptedArtworkCache{found: true, url: "https://c/" + placeholderArtHash, source: "caa"}, live: scriptedResolver{nameURL: "https://n.jpg"}},
			noDurable: true, noIndex: true,
			wantPath:  "none",
			wantCalls: []string{"cache.get:"},
		},
		{
			name:      "cached miss for an artist falls through to the live resolver",
			in:        domain.SearchResult{Kind: domain.ResultKindArtist, Title: "Kendrick Lamar"},
			st:        stages{cache: &scriptedArtworkCache{found: true}, live: scriptedResolver{nameURL: "https://n.jpg"}},
			noDurable: true, noIndex: true,
			wantPath:  "pending",
			wantCalls: []string{"cache.get:", "resolve.name:", "cache.set:|https://n.jpg|name-src|name"},
		},
		{
			name:      "live resolver miss caches the miss",
			in:        domain.SearchResult{Kind: domain.ResultKindTrack, Title: "Humble", Subtitle: "Kendrick Lamar", RecordingMBID: "own"},
			st:        stages{cache: &scriptedArtworkCache{}},
			noDurable: true, noIndex: true,
			wantPath:  "pending",
			wantCalls: []string{"cache.get:own", "resolve.identity:own|", "resolve.name:own", "cache.set:own|||none"},
		},
		{
			name:      "a miss from failing resolvers is never cached",
			in:        domain.SearchResult{Kind: domain.ResultKindTrack, Title: "Humble", Subtitle: "Kendrick Lamar", RecordingMBID: "own"},
			st:        stages{cache: &scriptedArtworkCache{}, live: scriptedResolver{outage: errors.New("coverartarchive 503")}},
			noDurable: true, noIndex: true,
			wantPath:  "pending",
			wantCalls: []string{"cache.get:own", "resolve.identity:own|", "resolve.name:own"},
		},
		{
			name:      "identity miss falls back to name lookup",
			in:        domain.SearchResult{Kind: domain.ResultKindTrack, Title: "Humble", Subtitle: "Kendrick Lamar", RecordingMBID: "own"},
			st:        stages{cache: &scriptedArtworkCache{}, live: scriptedResolver{nameURL: "https://n.jpg"}},
			noDurable: true, noIndex: true,
			wantPath:  "pending",
			wantCalls: []string{"cache.get:own", "resolve.identity:own|", "resolve.name:own", "cache.set:own|https://n.jpg|name-src|name"},
		},
		{
			name:      "without an artwork cache the live stage never runs",
			in:        domain.SearchResult{Kind: domain.ResultKindTrack, Title: "Humble", Subtitle: "Kendrick Lamar", RecordingMBID: "own"},
			st:        stages{live: scriptedResolver{nameURL: "https://n.jpg"}},
			noDurable: true, noIndex: true, noCache: true,
			wantPath:  "pending",
			wantCalls: nil,
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
			if tc.noDurable {
				store = artwork.NoIdentities{}
			}
			if tc.noIndex {
				index = artwork.NoMBIDIndex{}
			}
			bg := searchtest.NewTracker(t)
			var f *artwork.ArtworkFiller
			if tc.noCache {
				f = newPendingFiller(t, bg, store)
			} else {
				var err error
				f, err = artwork.NewArtworkFiller(&tc.st.live, tc.st.cache, store, index, bg)
				if err != nil {
					t.Fatalf("NewArtworkFiller: %v", err)
				}
			}

			got := fillAndSettle(t, bg, f, tc.in)[0]

			if path := got.ArtworkPath; path != domain.ArtworkPath(tc.wantPath) {
				t.Errorf("ArtworkPath = %q, want %q", path, tc.wantPath)
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
