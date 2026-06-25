package enrich

import (
	"context"
	"errors"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

type fakeLyricsProvider struct {
	resolves   int
	lookups    int
	lastArtist string
	lastTitle  string
	resolveID  string
	resolveErr error
	lookupErr  error
	lyrics     domain.DeezerLyrics
}

func (f *fakeLyricsProvider) ResolveTrackID(_ context.Context, artist, title string) (string, error) {
	f.resolves++
	f.lastArtist = artist
	f.lastTitle = title
	if f.resolveErr != nil {
		return "", f.resolveErr
	}
	return f.resolveID, nil
}

func (f *fakeLyricsProvider) Lookup(_ context.Context, _ string) (domain.DeezerLyrics, error) {
	f.lookups++
	if f.lookupErr != nil {
		return domain.EmptyDeezerLyrics(), f.lookupErr
	}
	return f.lyrics, nil
}

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
	provider := &fakeLyricsProvider{resolveID: "3135556", lyrics: sampleLyrics()}
	svc := NewLyricsService(provider, newMemLyricsCache())

	_, err := svc.Execute(context.Background(), "Hello", "Adele")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The subtitle is the artist; the title is the track.
	if provider.lastArtist != "Adele" || provider.lastTitle != "Hello" {
		t.Errorf("names: got artist=%q title=%q", provider.lastArtist, provider.lastTitle)
	}
}

func TestLyricsService_CacheHitShortCircuits(t *testing.T) {
	provider := &fakeLyricsProvider{resolveID: "3135556", lyrics: sampleLyrics()}
	cache := newMemLyricsCache()
	svc := NewLyricsService(provider, cache)

	_, _ = svc.Execute(context.Background(), "Hello", "Adele")
	if provider.lookups != 1 || cache.sets != 1 {
		t.Fatalf("setup: lookups=%d sets=%d", provider.lookups, cache.sets)
	}
	_, _ = svc.Execute(context.Background(), "Hello", "Adele")
	if provider.resolves != 1 || provider.lookups != 1 {
		t.Errorf("expected cache hit (no extra resolve/lookup), got resolves=%d lookups=%d",
			provider.resolves, provider.lookups)
	}
}

func TestLyricsService_UnresolvedCachedNegative(t *testing.T) {
	provider := &fakeLyricsProvider{resolveID: ""} // resolves to nothing
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
	if provider.lookups != 0 {
		t.Errorf("an unresolved id should not be looked up, got lookups=%d", provider.lookups)
	}
}

func TestLyricsService_NoLyricsCachedNegative(t *testing.T) {
	provider := &fakeLyricsProvider{resolveID: "1", lyrics: domain.EmptyDeezerLyrics()}
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
	provider := &fakeLyricsProvider{resolveID: "1", lookupErr: errors.New("pipe 500")}
	cache := newMemLyricsCache()
	svc := NewLyricsService(provider, cache)

	l, err := svc.Execute(context.Background(), "Hello", "Adele")
	if err != nil {
		t.Fatalf("error must be swallowed (best-effort), got %v", err)
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
