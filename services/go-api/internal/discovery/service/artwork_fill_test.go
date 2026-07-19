package service

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
)

type fakeArtworkResolver struct {
	url   string
	calls int32
}

func (r *fakeArtworkResolver) ResolveTagged(_ context.Context, _ domain.ResultKind, _, _, _ string) (string, string, error) {
	atomic.AddInt32(&r.calls, 1)
	return r.url, "", nil
}

func (r *fakeArtworkResolver) ResolveWithIdentityTagged(_ context.Context, _ domain.ResultKind, _, _ string, _ ports.ArtworkIdentity) (string, string, error) {
	return "", "", nil
}

type fakeArtworkCache struct {
	mu    sync.Mutex
	store map[string]string
}

func (c *fakeArtworkCache) Get(_ context.Context, _ domain.ResultKind, title, _, _ string) (string, string, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	url, ok := c.store[title]
	return url, "", ok, nil
}

func (c *fakeArtworkCache) Set(_ context.Context, _ domain.ResultKind, title, _, _, url, _ string, _ ports.ArtworkConfidence) error {
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

// capturingArtworkResolver records the mbid it was last called with.
type capturingArtworkResolver struct {
	url     string
	mu      sync.Mutex
	gotMBID string
}

func (r *capturingArtworkResolver) ResolveTagged(_ context.Context, _ domain.ResultKind, _, _, mbid string) (string, string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if mbid != "" {
		r.gotMBID = mbid
	}
	return r.url, "", nil
}

func (r *capturingArtworkResolver) ResolveWithIdentityTagged(_ context.Context, _ domain.ResultKind, _, _ string, _ ports.ArtworkIdentity) (string, string, error) {
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
	// A non-MB result with no mbid: the MBID index (warmed by detail-opens)
	// supplies one, which must reach the artwork resolver so its MBID-keyed tier
	// (CAA/Fanart) can fire on the search card.
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

func (f *fakeIdentityStore) LookupByProviderID(_ context.Context, _ domain.ResultKind, _, _ string) (string, map[string]string, bool) {
	atomic.AddInt32(&f.lookups, 1)
	if f.mbid == "" {
		return "", nil, false
	}
	return f.mbid, f.xref, true
}

func TestService_IdentityStoreResolvesArtworkWhenMBAbsent(t *testing.T) {
	// The deterministic fix: a provider-only result (MusicBrainz absent from this
	// fan-out, so merge stamped no xref) resolves its identity from the durable
	// store, keyed on its own provider id. The bridged ids + MBID reach the artwork
	// resolver so it stays identity-first — the right entity — even though MB never
	// answered this search.
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

// fakeIdentityAwareResolver resolves only when given a proven identity — exercising
// the identity-first branch so the resolution path is reported as identity.
type fakeIdentityAwareResolver struct{ url string }

func (r *fakeIdentityAwareResolver) ResolveTagged(_ context.Context, _ domain.ResultKind, _, _, _ string) (string, string, error) {
	return "", "", nil
}

func (r *fakeIdentityAwareResolver) ResolveWithIdentityTagged(_ context.Context, _ domain.ResultKind, _, _ string, id ports.ArtworkIdentity) (string, string, error) {
	if id.HasLinks() {
		return r.url, "", nil
	}
	return "", "", nil
}

func TestService_ArtworkPathIsDurableIdentityWhenStoreResolves(t *testing.T) {
	// When MB is absent (no xref in merge) but the durable store supplies identity,
	// and artwork then resolves identity-first, the resolution path stamped for the
	// operator console must read "durable-identity" — the fix, made visible.
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
