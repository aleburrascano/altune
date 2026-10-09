package enrich

import (
	"altune/go-api/internal/discovery/discoverytest"
	"altune/go-api/internal/discovery/domain"
	"context"
	"errors"
	"testing"
)

type memLastFmCache struct {
	pos  map[string]domain.LastFmEnrichment
	neg  map[string]bool
	gets int
	sets int
	negs int
}

func newMemLastFmCache() *memLastFmCache {
	return &memLastFmCache{pos: map[string]domain.LastFmEnrichment{}, neg: map[string]bool{}}
}

func (c *memLastFmCache) Get(_ context.Context, k string) (domain.LastFmEnrichment, bool, error) {
	c.gets++
	e, ok := c.pos[k]
	return e, ok, nil
}

func (c *memLastFmCache) Set(_ context.Context, k string, e domain.LastFmEnrichment) error {
	c.sets++
	c.pos[k] = e
	return nil
}

func (c *memLastFmCache) GetNegative(_ context.Context, k string) (bool, error) {
	return c.neg[k], nil
}

func (c *memLastFmCache) SetNegative(_ context.Context, k string) error {
	c.negs++
	c.neg[k] = true
	return nil
}

func sampleLastFmEnrichment() domain.LastFmEnrichment {
	e := domain.EmptyLastFmEnrichment()
	e.Listeners = 5172275
	e.Playcount = 1050884806
	e.Tags = []string{"Hip-Hop", "rap"}
	e.Similar = []string{"Baby Keem"}
	e.Bio = "American rapper."
	return e
}

func TestLastFmEnrichmentService_ArtistTranslatesNames(t *testing.T) {
	enricher := &discoverytest.LastFmEnricher{Enrichment: sampleLastFmEnrichment()}
	svc := NewLastFmEnrichmentService(enricher, newMemLastFmCache())

	_, err := svc.Execute(context.Background(), domain.ResultKindArtist, "Kendrick Lamar", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enricher.LastArtist != "Kendrick Lamar" || enricher.LastTitle != "" {
		t.Errorf("artist names: got artist=%q title=%q", enricher.LastArtist, enricher.LastTitle)
	}
}

func TestLastFmEnrichmentService_TrackTranslatesNames(t *testing.T) {
	enricher := &discoverytest.LastFmEnricher{Enrichment: sampleLastFmEnrichment()}
	svc := NewLastFmEnrichmentService(enricher, newMemLastFmCache())

	_, err := svc.Execute(context.Background(), domain.ResultKindTrack, "HUMBLE.", "Kendrick Lamar")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enricher.LastArtist != "Kendrick Lamar" || enricher.LastTitle != "HUMBLE." {
		t.Errorf("track names: got artist=%q title=%q", enricher.LastArtist, enricher.LastTitle)
	}
}

func TestLastFmEnrichmentService_CacheHitShortCircuits(t *testing.T) {
	enricher := &discoverytest.LastFmEnricher{Enrichment: sampleLastFmEnrichment()}
	cache := newMemLastFmCache()
	svc := NewLastFmEnrichmentService(enricher, cache)

	_, _ = svc.Execute(context.Background(), domain.ResultKindArtist, "Kendrick Lamar", "")
	if enricher.Lookups != 1 || cache.sets != 1 {
		t.Fatalf("setup: lookups=%d sets=%d", enricher.Lookups, cache.sets)
	}
	_, _ = svc.Execute(context.Background(), domain.ResultKindArtist, "Kendrick Lamar", "")
	if enricher.Lookups != 1 {
		t.Errorf("expected cache hit (no extra lookup), got lookups=%d", enricher.Lookups)
	}
}

func TestLastFmEnrichmentService_UnresolvedCachedNegative(t *testing.T) {
	enricher := &discoverytest.LastFmEnricher{Enrichment: domain.EmptyLastFmEnrichment()}
	cache := newMemLastFmCache()
	svc := NewLastFmEnrichmentService(enricher, cache)

	e, err := svc.Execute(context.Background(), domain.ResultKindArtist, "Nonexistent", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !e.IsZero() {
		t.Errorf("expected empty enrichment, got %+v", e)
	}
	if cache.negs != 1 {
		t.Errorf("expected a negative cache write, got negs=%d", cache.negs)
	}
}

func TestLastFmEnrichmentService_LookupErrorDegradesToEmpty(t *testing.T) {
	enricher := &discoverytest.LastFmEnricher{LookupErr: errors.New("boom")}
	cache := newMemLastFmCache()
	svc := NewLastFmEnrichmentService(enricher, cache)

	e, err := svc.Execute(context.Background(), domain.ResultKindArtist, "Kendrick Lamar", "")
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

func TestLastFmEnrichmentService_NilEnricher(t *testing.T) {
	svc := NewLastFmEnrichmentService(nil, nil)
	e, err := svc.Execute(context.Background(), domain.ResultKindArtist, "Kendrick Lamar", "")
	if err != nil || !e.IsZero() {
		t.Errorf("nil enricher should return empty + nil, got %+v err=%v", e, err)
	}
}
