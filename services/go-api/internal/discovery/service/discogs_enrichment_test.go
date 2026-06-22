package service

import (
	"context"
	"errors"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

type fakeDiscogsEnricher struct {
	resolves   int
	lookups    int
	masterID   int
	resolveErr error
	lookupErr  error
	enrichment domain.DiscogsEnrichment

	artistResolves   int
	artistLookups    int
	artistID         int
	artistEnrichment domain.DiscogsArtistEnrichment
}

func (f *fakeDiscogsEnricher) ResolveMasterID(_ context.Context, _, _ string) (int, error) {
	f.resolves++
	if f.resolveErr != nil {
		return 0, f.resolveErr
	}
	return f.masterID, nil
}

func (f *fakeDiscogsEnricher) LookupAlbum(_ context.Context, _ int) (domain.DiscogsEnrichment, error) {
	f.lookups++
	if f.lookupErr != nil {
		return domain.EmptyDiscogsEnrichment(), f.lookupErr
	}
	return f.enrichment, nil
}

func (f *fakeDiscogsEnricher) ResolveArtistID(_ context.Context, _ string) (int, error) {
	f.artistResolves++
	return f.artistID, nil
}

func (f *fakeDiscogsEnricher) LookupArtist(_ context.Context, _ int) (domain.DiscogsArtistEnrichment, error) {
	f.artistLookups++
	return f.artistEnrichment, nil
}

type memDiscogsArtistCache struct {
	pos  map[string]domain.DiscogsArtistEnrichment
	neg  map[string]bool
	sets int
}

func newMemDiscogsArtistCache() *memDiscogsArtistCache {
	return &memDiscogsArtistCache{pos: map[string]domain.DiscogsArtistEnrichment{}, neg: map[string]bool{}}
}
func (c *memDiscogsArtistCache) Get(_ context.Context, k string) (domain.DiscogsArtistEnrichment, bool, error) {
	e, ok := c.pos[k]
	return e, ok, nil
}
func (c *memDiscogsArtistCache) Set(_ context.Context, k string, e domain.DiscogsArtistEnrichment) error {
	c.sets++
	c.pos[k] = e
	return nil
}
func (c *memDiscogsArtistCache) GetNegative(_ context.Context, k string) (bool, error) {
	return c.neg[k], nil
}
func (c *memDiscogsArtistCache) SetNegative(_ context.Context, k string) error {
	c.neg[k] = true
	return nil
}

func sampleArtistEnrichment() domain.DiscogsArtistEnrichment {
	e := domain.EmptyDiscogsArtistEnrichment()
	e.ArtistID = 3062364
	e.Profile = "American rapper."
	e.Groups = []string{"Black Hippy"}
	return e
}

type memDiscogsCache struct {
	pos  map[string]domain.DiscogsEnrichment
	neg  map[string]bool
	gets int
	sets int
}

func newMemDiscogsCache() *memDiscogsCache {
	return &memDiscogsCache{pos: map[string]domain.DiscogsEnrichment{}, neg: map[string]bool{}}
}
func (c *memDiscogsCache) Get(_ context.Context, k string) (domain.DiscogsEnrichment, bool, error) {
	c.gets++
	e, ok := c.pos[k]
	return e, ok, nil
}
func (c *memDiscogsCache) Set(_ context.Context, k string, e domain.DiscogsEnrichment) error {
	c.sets++
	c.pos[k] = e
	return nil
}
func (c *memDiscogsCache) GetNegative(_ context.Context, k string) (bool, error) {
	return c.neg[k], nil
}
func (c *memDiscogsCache) SetNegative(_ context.Context, k string) error { c.neg[k] = true; return nil }

func sampleDiscogsEnrichment() domain.DiscogsEnrichment {
	e := domain.EmptyDiscogsEnrichment()
	e.MasterID = 1164779
	e.Styles = []string{"Conscious"}
	e.Year = 2017
	e.Credits = []domain.DiscogsCredit{{Name: "Bēkon", Role: "Producer"}}
	return e
}

func TestDiscogsEnrichmentService_Execute(t *testing.T) {
	t.Parallel()

	t.Run("resolves, looks up, and caches", func(t *testing.T) {
		enr := &fakeDiscogsEnricher{masterID: 1164779, enrichment: sampleDiscogsEnrichment()}
		cache := newMemDiscogsCache()
		svc := NewDiscogsEnrichmentService(enr, cache)

		got, err := svc.Execute(context.Background(), "Kendrick Lamar", "DAMN")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.MasterID != 1164779 {
			t.Errorf("master id: got %d", got.MasterID)
		}
		if enr.resolves != 1 || enr.lookups != 1 {
			t.Errorf("expected 1 resolve + 1 lookup, got %d/%d", enr.resolves, enr.lookups)
		}
		if cache.sets != 1 {
			t.Errorf("expected the result to be cached, sets=%d", cache.sets)
		}
	})

	t.Run("a positive cache hit skips resolve and lookup", func(t *testing.T) {
		enr := &fakeDiscogsEnricher{masterID: 1164779, enrichment: sampleDiscogsEnrichment()}
		cache := newMemDiscogsCache()
		cache.pos[discogsNameKey("Kendrick Lamar", "DAMN")] = sampleDiscogsEnrichment()
		svc := NewDiscogsEnrichmentService(enr, cache)

		got, err := svc.Execute(context.Background(), "Kendrick Lamar", "DAMN")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.MasterID != 1164779 {
			t.Errorf("master id: got %d", got.MasterID)
		}
		if enr.resolves != 0 || enr.lookups != 0 {
			t.Errorf("expected no network on cache hit, got %d/%d", enr.resolves, enr.lookups)
		}
	})

	t.Run("an unresolved master is negatively cached", func(t *testing.T) {
		enr := &fakeDiscogsEnricher{masterID: 0}
		cache := newMemDiscogsCache()
		svc := NewDiscogsEnrichmentService(enr, cache)

		got, err := svc.Execute(context.Background(), "Nobody", "No Such Album")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !got.IsZero() {
			t.Errorf("expected empty enrichment, got %+v", got)
		}
		if !cache.neg[discogsNameKey("Nobody", "No Such Album")] {
			t.Error("expected the miss to be negatively cached")
		}
		if enr.lookups != 0 {
			t.Errorf("expected no lookup on a 0 master, got %d", enr.lookups)
		}
	})

	t.Run("a negatively-cached name short-circuits", func(t *testing.T) {
		enr := &fakeDiscogsEnricher{masterID: 1164779, enrichment: sampleDiscogsEnrichment()}
		cache := newMemDiscogsCache()
		cache.neg[discogsNameKey("Nobody", "No Such Album")] = true
		svc := NewDiscogsEnrichmentService(enr, cache)

		got, err := svc.Execute(context.Background(), "Nobody", "No Such Album")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !got.IsZero() {
			t.Errorf("expected empty, got %+v", got)
		}
		if enr.resolves != 0 {
			t.Errorf("expected no resolve on negative hit, got %d", enr.resolves)
		}
	})

	t.Run("a lookup error degrades to empty without caching", func(t *testing.T) {
		enr := &fakeDiscogsEnricher{masterID: 1164779, lookupErr: errors.New("boom")}
		cache := newMemDiscogsCache()
		svc := NewDiscogsEnrichmentService(enr, cache)

		got, err := svc.Execute(context.Background(), "Kendrick Lamar", "DAMN")
		if err != nil {
			t.Fatalf("expected nil error (best-effort), got %v", err)
		}
		if !got.IsZero() {
			t.Errorf("expected empty on lookup error, got %+v", got)
		}
		if cache.sets != 0 {
			t.Errorf("expected nothing cached on error, sets=%d", cache.sets)
		}
	})

	t.Run("an empty album short-circuits", func(t *testing.T) {
		enr := &fakeDiscogsEnricher{}
		svc := NewDiscogsEnrichmentService(enr, nil)

		got, err := svc.Execute(context.Background(), "Kendrick Lamar", "  ")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !got.IsZero() || enr.resolves != 0 {
			t.Errorf("expected empty + no resolve, got %+v resolves=%d", got, enr.resolves)
		}
	})
}

func TestDiscogsArtistEnrichmentService_Execute(t *testing.T) {
	t.Parallel()

	t.Run("resolves, looks up, and caches", func(t *testing.T) {
		enr := &fakeDiscogsEnricher{artistID: 3062364, artistEnrichment: sampleArtistEnrichment()}
		cache := newMemDiscogsArtistCache()
		svc := NewDiscogsArtistEnrichmentService(enr, cache)

		got, err := svc.Execute(context.Background(), "Kendrick Lamar")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.ArtistID != 3062364 || got.Groups[0] != "Black Hippy" {
			t.Errorf("got %+v", got)
		}
		if enr.artistResolves != 1 || enr.artistLookups != 1 || cache.sets != 1 {
			t.Errorf("expected 1/1/1, got %d/%d/%d", enr.artistResolves, enr.artistLookups, cache.sets)
		}
	})

	t.Run("a positive cache hit skips the network", func(t *testing.T) {
		enr := &fakeDiscogsEnricher{artistID: 3062364, artistEnrichment: sampleArtistEnrichment()}
		cache := newMemDiscogsArtistCache()
		cache.pos[NormalizeForMatch("Kendrick Lamar")] = sampleArtistEnrichment()
		svc := NewDiscogsArtistEnrichmentService(enr, cache)

		got, err := svc.Execute(context.Background(), "Kendrick Lamar")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.ArtistID != 3062364 {
			t.Errorf("got %+v", got)
		}
		if enr.artistResolves != 0 || enr.artistLookups != 0 {
			t.Errorf("expected no network, got %d/%d", enr.artistResolves, enr.artistLookups)
		}
	})

	t.Run("an unresolved artist is negatively cached", func(t *testing.T) {
		enr := &fakeDiscogsEnricher{artistID: 0}
		cache := newMemDiscogsArtistCache()
		svc := NewDiscogsArtistEnrichmentService(enr, cache)

		got, err := svc.Execute(context.Background(), "Nobody At All")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !got.IsZero() {
			t.Errorf("expected empty, got %+v", got)
		}
		if !cache.neg[NormalizeForMatch("Nobody At All")] {
			t.Error("expected the miss to be negatively cached")
		}
	})

	t.Run("an empty name short-circuits", func(t *testing.T) {
		enr := &fakeDiscogsEnricher{}
		svc := NewDiscogsArtistEnrichmentService(enr, nil)

		got, err := svc.Execute(context.Background(), "   ")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !got.IsZero() || enr.artistResolves != 0 {
			t.Errorf("expected empty + no resolve, got %+v resolves=%d", got, enr.artistResolves)
		}
	})
}
