package enrich

import (
	"altune/go-api/internal/discovery/discoverytest"
	"altune/go-api/internal/discovery/domain"
	"context"
	"errors"
	"testing"
)

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
	enricher := &discoverytest.DeezerEnricher{ResolvedID: "1109731", Enrichment: sampleDeezerTrackEnrichment()}
	svc := NewDeezerEnrichmentService(enricher, newMemDeezerCache())

	_, err := svc.Execute(context.Background(), domain.ResultKindTrack, "Lose Yourself", "Eminem")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enricher.LastArtist != "Eminem" || enricher.LastTitle != "Lose Yourself" {
		t.Errorf("names: got artist=%q title=%q", enricher.LastArtist, enricher.LastTitle)
	}
}

func TestDeezerEnrichmentService_ArtistKindReturnsEmpty(t *testing.T) {
	enricher := &discoverytest.DeezerEnricher{ResolvedID: "27", Enrichment: sampleDeezerTrackEnrichment()}
	svc := NewDeezerEnrichmentService(enricher, newMemDeezerCache())

	e, err := svc.Execute(context.Background(), domain.ResultKindArtist, "Daft Punk", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !e.IsZero() {
		t.Errorf("artist kind should be empty, got %+v", e)
	}
	if enricher.Resolves != 0 {
		t.Errorf("artist kind should not resolve, got resolves=%d", enricher.Resolves)
	}
}

func TestDeezerEnrichmentService_CacheHitShortCircuits(t *testing.T) {
	enricher := &discoverytest.DeezerEnricher{ResolvedID: "1109731", Enrichment: sampleDeezerTrackEnrichment()}
	cache := newMemDeezerCache()
	svc := NewDeezerEnrichmentService(enricher, cache)

	_, _ = svc.Execute(context.Background(), domain.ResultKindTrack, "Lose Yourself", "Eminem")
	if enricher.Lookups != 1 || cache.sets != 1 {
		t.Fatalf("setup: lookups=%d sets=%d", enricher.Lookups, cache.sets)
	}
	_, _ = svc.Execute(context.Background(), domain.ResultKindTrack, "Lose Yourself", "Eminem")
	if enricher.Resolves != 1 || enricher.Lookups != 1 {
		t.Errorf("expected cache hit (no extra resolve/lookup), got resolves=%d lookups=%d",
			enricher.Resolves, enricher.Lookups)
	}
}

func TestDeezerEnrichmentService_UnresolvedCachedNegative(t *testing.T) {
	enricher := &discoverytest.DeezerEnricher{ResolvedID: ""}
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
	if enricher.Lookups != 0 {
		t.Errorf("an unresolved id should not be looked up, got lookups=%d", enricher.Lookups)
	}
}

func TestDeezerEnrichmentService_ResolveErrorDegradesToEmpty(t *testing.T) {
	enricher := &discoverytest.DeezerEnricher{ResolveErr: errors.New("boom")}
	cache := newMemDeezerCache()
	svc := NewDeezerEnrichmentService(enricher, cache)

	e, err := svc.Execute(context.Background(), domain.ResultKindAlbum, "Discovery", "Daft Punk")
	if !errors.Is(err, ErrDegraded) {
		t.Fatalf("a fetch error must surface as ErrDegraded (distinct from no data), got %v", err)
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
