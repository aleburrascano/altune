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

func (r *fakeArtworkResolver) Resolve(_ context.Context, _ domain.ResultKind, _, _, _ string) (string, error) {
	atomic.AddInt32(&r.calls, 1)
	return r.url, nil
}

type fakeArtworkCache struct {
	mu    sync.Mutex
	store map[string]string
}

func (c *fakeArtworkCache) Get(_ context.Context, _ domain.ResultKind, title, _, _ string) (string, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	url, ok := c.store[title]
	return url, ok, nil
}

func (c *fakeArtworkCache) Set(_ context.Context, _ domain.ResultKind, title, _, _ string, url string) error {
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

func (r *capturingArtworkResolver) Resolve(_ context.Context, _ domain.ResultKind, _, _, mbid string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if mbid != "" {
		r.gotMBID = mbid
	}
	return r.url, nil
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
