package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/textnorm"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

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
