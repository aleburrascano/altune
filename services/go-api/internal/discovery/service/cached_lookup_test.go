package service

import (
	"context"
	"errors"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

// cached_lookup_test exercises the deep read-through module CachedLookup directly
// (it was previously re-proven only transitively through the five provider
// enrichment services, each shipping an identical in-memory fake). One generic
// fake here tests the seam once.

// namedCache is a generic in-memory NameKeyedCache[T] for tests — the one fake
// that replaces the five per-provider copies in the enrichment tests.
type namedCache[T any] struct {
	pos     map[string]T
	neg     map[string]bool
	sets    int
	negSets int
}

func newNamedCache[T any]() *namedCache[T] {
	return &namedCache[T]{pos: map[string]T{}, neg: map[string]bool{}}
}

func (c *namedCache[T]) Get(_ context.Context, k string) (T, bool, error) {
	v, ok := c.pos[k]
	return v, ok, nil
}
func (c *namedCache[T]) Set(_ context.Context, k string, v T) error {
	c.pos[k] = v
	c.sets++
	return nil
}
func (c *namedCache[T]) GetNegative(_ context.Context, k string) (bool, error) {
	return c.neg[k], nil
}
func (c *namedCache[T]) SetNegative(_ context.Context, k string) error {
	c.neg[k] = true
	c.negSets++
	return nil
}

func TestCachedLookup_PositiveHitShortCircuits(t *testing.T) {
	cache := newNamedCache[string]()
	cache.pos["k"] = "cached"
	fetchCalled := false
	got, err := CachedLookup(context.Background(), cache, "k", "", func(context.Context) (string, bool, error) {
		fetchCalled = true
		return "fresh", true, nil
	})
	if err != nil || got != "cached" {
		t.Fatalf("got (%q,%v), want (cached,nil)", got, err)
	}
	if fetchCalled {
		t.Fatalf("a positive cache hit must not call fetch")
	}
}

func TestCachedLookup_NegativeHitReturnsEmptyWithoutFetch(t *testing.T) {
	cache := newNamedCache[string]()
	cache.neg["k"] = true
	fetchCalled := false
	got, err := CachedLookup(context.Background(), cache, "k", "EMPTY", func(context.Context) (string, bool, error) {
		fetchCalled = true
		return "fresh", true, nil
	})
	if err != nil || got != "EMPTY" {
		t.Fatalf("got (%q,%v), want (EMPTY,nil)", got, err)
	}
	if fetchCalled {
		t.Fatalf("a negative cache hit must not call fetch")
	}
}

func TestCachedLookup_DefinitiveMissNegativeCaches(t *testing.T) {
	cache := newNamedCache[string]()
	got, err := CachedLookup(context.Background(), cache, "k", "EMPTY", func(context.Context) (string, bool, error) {
		return "", false, nil // definitive miss
	})
	if err != nil || got != "EMPTY" {
		t.Fatalf("got (%q,%v), want (EMPTY,nil)", got, err)
	}
	if cache.negSets != 1 {
		t.Fatalf("a definitive miss must negative-cache the name, negSets=%d", cache.negSets)
	}
}

func TestCachedLookup_TransientErrorIsNotCached(t *testing.T) {
	cache := newNamedCache[string]()
	got, err := CachedLookup(context.Background(), cache, "k", "EMPTY", func(context.Context) (string, bool, error) {
		return "", false, errors.New("network blip")
	})
	if err != nil || got != "EMPTY" {
		t.Fatalf("got (%q,%v), want (EMPTY,nil) — transient errors are swallowed", got, err)
	}
	if cache.negSets != 0 || cache.sets != 0 {
		t.Fatalf("a transient error must NOT be cached (sets=%d negSets=%d)", cache.sets, cache.negSets)
	}
}

func TestCachedLookup_PositiveResultIsCached(t *testing.T) {
	cache := newNamedCache[string]()
	got, err := CachedLookup(context.Background(), cache, "k", "", func(context.Context) (string, bool, error) {
		return "fresh", true, nil
	})
	if err != nil || got != "fresh" {
		t.Fatalf("got (%q,%v), want (fresh,nil)", got, err)
	}
	if v := cache.pos["k"]; v != "fresh" {
		t.Fatalf("a positive result must be cached, got %q", v)
	}
}

func TestCachedLookup_NilCacheRunsUncached(t *testing.T) {
	got, err := CachedLookup[string](context.Background(), nil, "k", "EMPTY", func(context.Context) (string, bool, error) {
		return "fresh", true, nil
	})
	if err != nil || got != "fresh" {
		t.Fatalf("got (%q,%v), want (fresh,nil) with a nil cache", got, err)
	}
}

// --- fillArtwork / disambiguation never-reorder (the display bracket fills
// fields, it must not change the ranked order). Reuses fakeArtworkResolver
// from artwork_fill_test. ---

func TestFillArtwork_FillsArtworkWithoutReordering(t *testing.T) {
	resolver := &fakeArtworkResolver{url: "art://cover.jpg"}
	s := NewService(nil, NewCircuitBreaker(), WithArtworkResolver(resolver))
	in := []domain.SearchResult{
		deezerTrack("Alpha", "A", 30),
		deezerTrack("Bravo", "B", 20),
		deezerTrack("Charlie", "C", 10),
	}
	got := s.fillArtwork(context.Background(), in)

	want := []string{"Alpha", "Bravo", "Charlie"}
	if len(got) != len(want) {
		t.Fatalf("fillArtwork changed length: %v", titles(got))
	}
	for i, title := range want {
		if got[i].Title != title {
			t.Fatalf("fillArtwork reordered results: got %v, want %v", titles(got), want)
		}
		if got[i].ImageURL != "art://cover.jpg" {
			t.Fatalf("fillArtwork did not fill artwork for %q (got %q) — test is not exercising enrichment", title, got[i].ImageURL)
		}
	}
}

func TestApplyArtistDisambiguation_FillsSubtitleWithoutReordering(t *testing.T) {
	s := NewService(nil, NewCircuitBreaker()) // nil albumValidator → extras-only branch
	in := []domain.SearchResult{
		res(domain.ResultKindArtist, "Nas", "", domain.ProviderDeezer, map[string]any{"disambiguation": "American rapper"}),
		deezerTrack("Some Song", "Nas", 50),
		res(domain.ResultKindArtist, "Genesis", "", domain.ProviderDeezer, map[string]any{"disambiguation": "English rock band"}),
	}
	got := s.applyArtistDisambiguation(context.Background(), in)

	want := []string{"Nas", "Some Song", "Genesis"}
	for i, title := range want {
		if got[i].Title != title {
			t.Fatalf("disambiguation reordered results: got %v, want %v", titles(got), want)
		}
	}
	if got[0].Subtitle != "American rapper" || got[2].Subtitle != "English rock band" {
		t.Fatalf("disambiguation did not fill subtitles: %q / %q", got[0].Subtitle, got[2].Subtitle)
	}
}
