package enrich

import (
	"altune/go-api/internal/discovery/discoverytest"
	"altune/go-api/internal/discovery/domain"
	"context"
	"errors"
	"testing"
)

type memLyricsCache struct {
	pos  map[string]domain.DeezerLyrics
	neg  map[string]bool
	sets int
	negs int
}

func newMemLyricsCache() *memLyricsCache {
	return &memLyricsCache{pos: map[string]domain.DeezerLyrics{}, neg: map[string]bool{}}
}

func (c *memLyricsCache) Get(_ context.Context, k string) (domain.DeezerLyrics, bool, error) {
	l, ok := c.pos[k]
	return l, ok, nil
}

func (c *memLyricsCache) Set(_ context.Context, k string, l domain.DeezerLyrics) error {
	c.sets++
	c.pos[k] = l
	return nil
}

func (c *memLyricsCache) GetNegative(_ context.Context, k string) (bool, error) {
	return c.neg[k], nil
}

func (c *memLyricsCache) SetNegative(_ context.Context, k string) error {
	c.negs++
	c.neg[k] = true
	return nil
}

func sampleLyrics() domain.DeezerLyrics {
	l := domain.EmptyDeezerLyrics()
	l.Plain = "Hello, it's me"
	l.SyncedLines = []domain.SyncedLyricLine{{Timecode: "[00:12.34]", Line: "Hello, it's me", Milliseconds: 12340, Duration: 2000}}
	l.Writers = []string{"Adele Laurie Blue Adkins"}
	return l
}

func TestLyricsService_TranslatesNames(t *testing.T) {
	provider := &discoverytest.LyricsProvider{ResolvedID: "3135556", Lyrics: sampleLyrics()}
	svc := NewLyricsService(provider, newMemLyricsCache())

	_, err := svc.Execute(context.Background(), "Hello", "Adele")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider.LastArtist != "Adele" || provider.LastTitle != "Hello" {
		t.Errorf("names: got artist=%q title=%q", provider.LastArtist, provider.LastTitle)
	}
}

func TestLyricsService_CacheHitShortCircuits(t *testing.T) {
	provider := &discoverytest.LyricsProvider{ResolvedID: "3135556", Lyrics: sampleLyrics()}
	cache := newMemLyricsCache()
	svc := NewLyricsService(provider, cache)

	_, _ = svc.Execute(context.Background(), "Hello", "Adele")
	if provider.Lookups != 1 || cache.sets != 1 {
		t.Fatalf("setup: lookups=%d sets=%d", provider.Lookups, cache.sets)
	}
	_, _ = svc.Execute(context.Background(), "Hello", "Adele")
	if provider.Resolves != 1 || provider.Lookups != 1 {
		t.Errorf("expected cache hit (no extra resolve/lookup), got resolves=%d lookups=%d",
			provider.Resolves, provider.Lookups)
	}
}

func TestLyricsService_UnresolvedCachedNegative(t *testing.T) {
	provider := &discoverytest.LyricsProvider{ResolvedID: ""}
	cache := newMemLyricsCache()
	svc := NewLyricsService(provider, cache)

	l, err := svc.Execute(context.Background(), "Nonexistent", "Nobody")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !l.IsZero() {
		t.Errorf("expected empty lyrics, got %+v", l)
	}
	if cache.negs != 1 {
		t.Errorf("expected a negative cache write, got negs=%d", cache.negs)
	}
	if provider.Lookups != 0 {
		t.Errorf("an unresolved id should not be looked up, got lookups=%d", provider.Lookups)
	}
}

func TestLyricsService_NoLyricsCachedNegative(t *testing.T) {
	provider := &discoverytest.LyricsProvider{ResolvedID: "1", Lyrics: domain.EmptyDeezerLyrics()}
	cache := newMemLyricsCache()
	svc := NewLyricsService(provider, cache)

	l, err := svc.Execute(context.Background(), "Instrumental", "Someone")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !l.IsZero() {
		t.Errorf("expected empty lyrics, got %+v", l)
	}
	if cache.negs != 1 {
		t.Errorf("a track with no lyrics should be negative-cached, got negs=%d", cache.negs)
	}
}

func TestLyricsService_LookupErrorDegradesWithoutPoisoning(t *testing.T) {
	provider := &discoverytest.LyricsProvider{ResolvedID: "1", LookupErr: errors.New("pipe 500")}
	cache := newMemLyricsCache()
	svc := NewLyricsService(provider, cache)

	l, err := svc.Execute(context.Background(), "Hello", "Adele")
	if !errors.Is(err, ErrDegraded) {
		t.Fatalf("a fetch error must surface as ErrDegraded (distinct from no data), got %v", err)
	}
	if !l.IsZero() {
		t.Errorf("expected empty lyrics on error, got %+v", l)
	}
	if cache.negs != 0 {
		t.Errorf("a transient lookup error must not poison the cache, got negs=%d", cache.negs)
	}
}

func TestLyricsService_NilProvider(t *testing.T) {
	svc := NewLyricsService(nil, nil)
	l, err := svc.Execute(context.Background(), "Hello", "Adele")
	if err != nil || !l.IsZero() {
		t.Errorf("nil provider should return empty + nil, got %+v err=%v", l, err)
	}
}
