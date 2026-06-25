package enrich

import (
	"context"
	"errors"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

type fakeDeezerEnricher struct {
	resolves   int
	lookups    int
	lastKind   domain.ResultKind
	lastArtist string
	lastTitle  string
	resolveID  string
	resolveErr error
	lookupErr  error
	enrichment domain.DeezerEnrichment
}

func (f *fakeDeezerEnricher) ResolveID(
	_ context.Context,
	kind domain.ResultKind,
	artist, title string,
) (string, error) {
	f.resolves++
	f.lastKind = kind
	f.lastArtist = artist
	f.lastTitle = title
	if f.resolveErr != nil {
		return "", f.resolveErr
	}
	return f.resolveID, nil
}

func (f *fakeDeezerEnricher) Lookup(
	_ context.Context,
	_ domain.ResultKind,
	_ string,
) (domain.DeezerEnrichment, error) {
	f.lookups++
	if f.lookupErr != nil {
		return domain.EmptyDeezerEnrichment(), f.lookupErr
	}
	return f.enrichment, nil
}

type memDeezerCache struct {
	pos  map[string]domain.DeezerEnrichment
	neg  map[string]bool
	sets int
	negs int
}

func newMemDeezerCache() *memDeezerCache {
	return &memDeezerCache{pos: map[string]domain.DeezerEnrichment{}, neg: map[string]bool{}}
}
func (c *memDeezerCache) Get(_ context.Context, k string) (domain.DeezerEnrichment, bool, error) {
	e, ok := c.pos[k]
	return e, ok, nil
}
func (c *memDeezerCache) Set(_ context.Context, k string, e domain.DeezerEnrichment) error {
	c.sets++
	c.pos[k] = e
	return nil
}
func (c *memDeezerCache) GetNegative(_ context.Context, k string) (bool, error) {
	return c.neg[k], nil
}
func (c *memDeezerCache) SetNegative(_ context.Context, k string) error {
	c.negs++
	c.neg[k] = true
	return nil
}

func sampleDeezerTrackEnrichment() domain.DeezerEnrichment {
	e := domain.EmptyDeezerEnrichment()
	e.BPM = 172
	e.Gain = -8.3
	e.Explicit = true
	return e
}

func TestDeezerEnrichmentService_TrackTranslatesNames(t *testing.T) {
	enricher := &fakeDeezerEnricher{resolveID: "1109731", enrichment: sampleDeezerTrackEnrichment()}
	svc := NewDeezerEnrichmentService(enricher, newMemDeezerCache())

	_, err := svc.Execute(context.Background(), domain.ResultKindTrack, "Lose Yourself", "Eminem")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The subtitle is the artist; the title is the entity.
	if enricher.lastArtist != "Eminem" || enricher.lastTitle != "Lose Yourself" {
		t.Errorf("names: got artist=%q title=%q", enricher.lastArtist, enricher.lastTitle)
	}
}

func TestDeezerEnrichmentService_ArtistKindReturnsEmpty(t *testing.T) {
	enricher := &fakeDeezerEnricher{resolveID: "27", enrichment: sampleDeezerTrackEnrichment()}
	svc := NewDeezerEnrichmentService(enricher, newMemDeezerCache())

	e, err := svc.Execute(context.Background(), domain.ResultKindArtist, "Daft Punk", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !e.IsZero() {
		t.Errorf("artist kind should be empty, got %+v", e)
	}
	if enricher.resolves != 0 {
		t.Errorf("artist kind should not resolve, got resolves=%d", enricher.resolves)
	}
}

func TestDeezerEnrichmentService_CacheHitShortCircuits(t *testing.T) {
	enricher := &fakeDeezerEnricher{resolveID: "1109731", enrichment: sampleDeezerTrackEnrichment()}
	cache := newMemDeezerCache()
	svc := NewDeezerEnrichmentService(enricher, cache)

	_, _ = svc.Execute(context.Background(), domain.ResultKindTrack, "Lose Yourself", "Eminem")
	if enricher.lookups != 1 || cache.sets != 1 {
		t.Fatalf("setup: lookups=%d sets=%d", enricher.lookups, cache.sets)
	}
	_, _ = svc.Execute(context.Background(), domain.ResultKindTrack, "Lose Yourself", "Eminem")
	if enricher.resolves != 1 || enricher.lookups != 1 {
		t.Errorf("expected cache hit (no extra resolve/lookup), got resolves=%d lookups=%d",
			enricher.resolves, enricher.lookups)
	}
}

func TestDeezerEnrichmentService_UnresolvedCachedNegative(t *testing.T) {
	enricher := &fakeDeezerEnricher{resolveID: ""} // resolves to nothing
	cache := newMemDeezerCache()
	svc := NewDeezerEnrichmentService(enricher, cache)

	e, err := svc.Execute(context.Background(), domain.ResultKindTrack, "Nonexistent", "Nobody")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !e.IsZero() {
		t.Errorf("expected empty enrichment, got %+v", e)
	}
	if cache.negs != 1 {
		t.Errorf("expected a negative cache write, got negs=%d", cache.negs)
	}
	if enricher.lookups != 0 {
		t.Errorf("an unresolved id should not be looked up, got lookups=%d", enricher.lookups)
	}
}

func TestDeezerEnrichmentService_ResolveErrorDegradesToEmpty(t *testing.T) {
	enricher := &fakeDeezerEnricher{resolveErr: errors.New("boom")}
	cache := newMemDeezerCache()
	svc := NewDeezerEnrichmentService(enricher, cache)

	e, err := svc.Execute(context.Background(), domain.ResultKindAlbum, "Discovery", "Daft Punk")
	if err != nil {
		t.Fatalf("error must be swallowed (best-effort), got %v", err)
	}
	if !e.IsZero() {
		t.Errorf("expected empty enrichment on error, got %+v", e)
	}
	if cache.negs != 0 {
		t.Errorf("transient error should not poison the cache, got negs=%d", cache.negs)
	}
}

func TestDeezerEnrichmentService_NilEnricher(t *testing.T) {
	svc := NewDeezerEnrichmentService(nil, nil)
	e, err := svc.Execute(context.Background(), domain.ResultKindTrack, "Lose Yourself", "Eminem")
	if err != nil || !e.IsZero() {
		t.Errorf("nil enricher should return empty + nil, got %+v err=%v", e, err)
	}
}
