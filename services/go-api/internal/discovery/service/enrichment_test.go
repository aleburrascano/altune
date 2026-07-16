package service

import (
	"context"
	"errors"
	"testing"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
)

type fakeEnricher struct {
	lookups   int
	resolves  int
	lookupErr error
	resolveID string
	enrichment domain.MBEnrichment
}

func (f *fakeEnricher) ResolveMBID(_ context.Context, _ domain.ResultKind, _, _ string) (string, error) {
	f.resolves++
	return f.resolveID, nil
}

func (f *fakeEnricher) Lookup(_ context.Context, _ domain.ResultKind, _ string) (domain.MBEnrichment, error) {
	f.lookups++
	if f.lookupErr != nil {
		return domain.EmptyEnrichment(), f.lookupErr
	}
	return f.enrichment, nil
}

type fakeArtwork struct {
	calls int
	url   string
}

func (f *fakeArtwork) ResolveTagged(_ context.Context, _ domain.ResultKind, _, _, _ string) (string, string, error) {
	f.calls++
	return f.url, "", nil
}

func (f *fakeArtwork) ResolveWithIdentityTagged(_ context.Context, _ domain.ResultKind, _, _ string, _ ports.ArtworkIdentity) (string, string, error) {
	return "", "", nil
}

// memEnrichmentCache is an in-memory EnrichmentCache.
type memEnrichmentCache struct {
	pos map[string]domain.MBEnrichment
	neg map[string]bool
}

func newMemCache() *memEnrichmentCache {
	return &memEnrichmentCache{pos: map[string]domain.MBEnrichment{}, neg: map[string]bool{}}
}
func (c *memEnrichmentCache) Get(_ context.Context, kind domain.ResultKind, mbid string) (domain.MBEnrichment, bool, error) {
	e, ok := c.pos[kind.String()+"|"+mbid]
	return e, ok, nil
}
func (c *memEnrichmentCache) Set(_ context.Context, kind domain.ResultKind, mbid string, e domain.MBEnrichment) error {
	c.pos[kind.String()+"|"+mbid] = e
	return nil
}
func (c *memEnrichmentCache) GetNegative(_ context.Context, kind domain.ResultKind, nameKey string) (bool, error) {
	return c.neg[kind.String()+"|"+nameKey], nil
}
func (c *memEnrichmentCache) SetNegative(_ context.Context, kind domain.ResultKind, nameKey string) error {
	c.neg[kind.String()+"|"+nameKey] = true
	return nil
}

type fakeMBIDMemo struct {
	remembered map[string]string
}

func (m *fakeMBIDMemo) LookupMBID(_ context.Context, _ domain.ResultKind, _ string) (string, bool) {
	return "", false
}
func (m *fakeMBIDMemo) RememberMBID(_ context.Context, kind domain.ResultKind, nameKey, mbid string) error {
	m.remembered[kind.String()+"|"+nameKey] = mbid
	return nil
}

func TestEnrichmentService_ResolveWarmsMBIDMemo(t *testing.T) {
	// A name-resolved (non-passed) MBID is memoized so the search path can attach
	// it to a non-MB result later (cap 5 warm).
	enr := &fakeEnricher{resolveID: "mbid-warm", enrichment: sampleEnrichment()}
	memo := &fakeMBIDMemo{remembered: map[string]string{}}
	svc := NewEnrichmentService(enr, &fakeArtwork{}, newMemCache(), WithMBIDMemo(memo))

	if _, err := svc.Execute(context.Background(), domain.ResultKindAlbum, "DAMN.", "Kendrick Lamar", ""); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(memo.remembered) != 1 {
		t.Fatalf("expected 1 remembered name→mbid mapping, got %d", len(memo.remembered))
	}
	for _, v := range memo.remembered {
		if v != "mbid-warm" {
			t.Errorf("remembered mbid = %q, want mbid-warm", v)
		}
	}
}

func sampleEnrichment() domain.MBEnrichment {
	e := domain.EmptyEnrichment()
	e.MBID = "mbid-1"
	e.Genres = []string{"hip hop"}
	e.Year = 2017
	return e
}

func TestEnrichmentService_PassedMBID_CachesWholeValue(t *testing.T) {
	enr := &fakeEnricher{enrichment: sampleEnrichment()}
	art := &fakeArtwork{url: "https://caa/1200.jpg"}
	cache := newMemCache()
	svc := NewEnrichmentService(enr, art, cache)

	got, err := svc.Execute(context.Background(), domain.ResultKindAlbum, "DAMN.", "Kendrick Lamar", "mbid-1")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got.ArtworkURL != "https://caa/1200.jpg" || got.Year != 2017 {
		t.Errorf("merged result wrong: %#v", got)
	}

	// AC#5: second identical call hits cache — no extra lookup, no extra artwork.
	got2, _ := svc.Execute(context.Background(), domain.ResultKindAlbum, "DAMN.", "Kendrick Lamar", "mbid-1")
	if got2.ArtworkURL != "https://caa/1200.jpg" {
		t.Errorf("cached value lost artwork: %#v", got2)
	}
	if enr.lookups != 1 {
		t.Errorf("lookups = %d, want 1 (second served from cache)", enr.lookups)
	}
	if art.calls != 1 {
		t.Errorf("artwork calls = %d, want 1 (cached value includes artwork_url)", art.calls)
	}
}

func TestEnrichmentService_LookupError_DegradesToEmpty(t *testing.T) {
	enr := &fakeEnricher{lookupErr: errors.New("mb 503")}
	cache := newMemCache()
	svc := NewEnrichmentService(enr, &fakeArtwork{}, cache)

	got, err := svc.Execute(context.Background(), domain.ResultKindArtist, "X", "", "mbid-err")
	if err != nil {
		t.Fatalf("must not surface error, got %v", err)
	}
	if !got.IsZero() {
		t.Errorf("want empty enrichment on lookup error, got %#v", got)
	}
	// Must not poison the positive cache with the failure.
	if _, found, _ := cache.Get(context.Background(), domain.ResultKindArtist, "mbid-err"); found {
		t.Error("lookup error must not be cached")
	}
}

func TestEnrichmentService_PassedMBID_404DegradesToEmpty(t *testing.T) {
	// AC#6 explicit 404 path: a passed mbid whose lookup 404s (modeled as a
	// lookup error) returns empty + nil error, same as any non-200.
	enr := &fakeEnricher{lookupErr: errors.New("musicbrainz returned 404")}
	svc := NewEnrichmentService(enr, nil, nil)

	got, err := svc.Execute(context.Background(), domain.ResultKindAlbum, "Stale", "", "stale-mbid")
	if err != nil || !got.IsZero() {
		t.Errorf("404 lookup must degrade to empty+nil, got %#v err=%v", got, err)
	}
}

func TestEnrichmentService_ArtworkMerged(t *testing.T) {
	enr := &fakeEnricher{enrichment: domain.EmptyEnrichment()}
	art := &fakeArtwork{url: "https://caa/front-1200.jpg"}
	svc := NewEnrichmentService(enr, art, nil)

	got, _ := svc.Execute(context.Background(), domain.ResultKindAlbum, "T", "A", "mbid-art")
	if got.ArtworkURL != "https://caa/front-1200.jpg" {
		t.Errorf("artwork_url = %q, want chain result", got.ArtworkURL)
	}
}

func TestEnrichmentService_Unresolved_NegativeCached(t *testing.T) {
	enr := &fakeEnricher{resolveID: ""} // name resolves to nothing
	cache := newMemCache()
	svc := NewEnrichmentService(enr, &fakeArtwork{}, cache)

	got, _ := svc.Execute(context.Background(), domain.ResultKindAlbum, "Unknown", "Nobody", "")
	if !got.IsZero() {
		t.Errorf("want empty on no resolve, got %#v", got)
	}
	// AC#5 negative path: a repeat must not re-run resolution.
	_, _ = svc.Execute(context.Background(), domain.ResultKindAlbum, "Unknown", "Nobody", "")
	if enr.resolves != 1 {
		t.Errorf("resolves = %d, want 1 (miss negatively cached)", enr.resolves)
	}
	if enr.lookups != 0 {
		t.Errorf("lookups = %d, want 0 (never resolved an mbid)", enr.lookups)
	}
}
