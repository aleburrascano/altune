package details_test

import (
	"altune/go-api/internal/search/internal/core/ports"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/service/details"
	"altune/go-api/internal/search/searchtest"
	"context"
	"errors"
	"strings"
	"testing"
)

func sampleDeezerTrackEnrichment() domain.DeezerEnrichment {
	e := domain.EmptyDeezerEnrichment()
	e.BPM = 172
	e.Gain = -8.3
	e.Explicit = true
	return e
}

func TestDeezerEnrichmentService_TrackTranslatesNames(t *testing.T) {
	enricher := &searchtest.DeezerEnricher{ResolvedID: "1109731", Enrichment: sampleDeezerTrackEnrichment()}
	svc := searchtest.NewDeezerEnrichment(t, enricher, searchtest.NewNameKeyedCache[domain.DeezerEnrichment]())

	_, err := svc.Execute(context.Background(), domain.ResultKindTrack, "Lose Yourself", "Eminem")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enricher.LastArtist != "Eminem" || enricher.LastTitle != "Lose Yourself" {
		t.Errorf("names: got artist=%q title=%q", enricher.LastArtist, enricher.LastTitle)
	}
}

func TestDeezerEnrichmentService_ArtistKindReturnsEmpty(t *testing.T) {
	enricher := &searchtest.DeezerEnricher{ResolvedID: "27", Enrichment: sampleDeezerTrackEnrichment()}
	svc := searchtest.NewDeezerEnrichment(t, enricher, searchtest.NewNameKeyedCache[domain.DeezerEnrichment]())

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
	enricher := &searchtest.DeezerEnricher{ResolvedID: "1109731", Enrichment: sampleDeezerTrackEnrichment()}
	cache := searchtest.NewNameKeyedCache[domain.DeezerEnrichment]()
	svc := searchtest.NewDeezerEnrichment(t, enricher, cache)

	_, _ = svc.Execute(context.Background(), domain.ResultKindTrack, "Lose Yourself", "Eminem")
	if enricher.Lookups != 1 || cache.Sets() != 1 {
		t.Fatalf("setup: lookups=%d sets=%d", enricher.Lookups, cache.Sets())
	}
	_, _ = svc.Execute(context.Background(), domain.ResultKindTrack, "Lose Yourself", "Eminem")
	if enricher.Resolves != 1 || enricher.Lookups != 1 {
		t.Errorf("expected cache hit (no extra resolve/lookup), got resolves=%d lookups=%d",
			enricher.Resolves, enricher.Lookups)
	}
}

func TestDeezerEnrichmentService_UnresolvedCachedNegative(t *testing.T) {
	enricher := &searchtest.DeezerEnricher{ResolvedID: ""}
	cache := searchtest.NewNameKeyedCache[domain.DeezerEnrichment]()
	svc := searchtest.NewDeezerEnrichment(t, enricher, cache)

	e, err := svc.Execute(context.Background(), domain.ResultKindTrack, "Nonexistent", "Nobody")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !e.IsZero() {
		t.Errorf("expected empty enrichment, got %+v", e)
	}
	if cache.Negatives() != 1 {
		t.Errorf("expected a negative cache write, got negs=%d", cache.Negatives())
	}
	if enricher.Lookups != 0 {
		t.Errorf("an unresolved id should not be looked up, got lookups=%d", enricher.Lookups)
	}
}

func TestDeezerEnrichmentService_ResolveErrorDegradesToEmpty(t *testing.T) {
	enricher := &searchtest.DeezerEnricher{ResolveErr: errors.New("boom")}
	cache := searchtest.NewNameKeyedCache[domain.DeezerEnrichment]()
	svc := searchtest.NewDeezerEnrichment(t, enricher, cache)

	e, err := svc.Execute(context.Background(), domain.ResultKindAlbum, "Discovery", "Daft Punk")
	if !errors.Is(err, details.ErrDegraded) {
		t.Fatalf("a fetch error must surface as ErrDegraded (distinct from no data), got %v", err)
	}
	if !e.IsZero() {
		t.Errorf("expected empty enrichment on error, got %+v", e)
	}
	if cache.Negatives() != 0 {
		t.Errorf("transient error should not poison the cache, got negs=%d", cache.Negatives())
	}
}

func TestDeezerEnrichmentService_RefusesAMissingCollaborator(t *testing.T) {
	enricher := &searchtest.DeezerEnricher{}
	cache := searchtest.NewNameKeyedCache[domain.DeezerEnrichment]()
	tests := []struct {
		name     string
		enricher ports.DeezerEnricher
		cache    ports.DeezerEnrichmentCache
		missing  string
	}{
		{"enricher", nil, cache, "deezer enricher"},
		{"cache", enricher, nil, "deezer cache"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, err := details.NewDeezerEnrichmentService(tt.enricher, tt.cache)

			if svc != nil || !errors.Is(err, details.ErrCollaboratorRequired) || !strings.Contains(err.Error(), tt.missing) {
				t.Fatalf("got (%v, %v), want a refusal naming %q", svc, err, tt.missing)
			}
		})
	}
}

func TestDeezerEnrichmentService_AnAbsentCacheStillResolves(t *testing.T) {
	enricher := &searchtest.DeezerEnricher{ResolvedID: "1", Enrichment: sampleDeezerTrackEnrichment()}
	svc := searchtest.NewDeezerEnrichment(t, enricher, details.NoNameKeyedCache[domain.DeezerEnrichment]{})

	e, err := svc.Execute(context.Background(), domain.ResultKindTrack, "Lose Yourself", "Eminem")

	if err != nil || e.IsZero() {
		t.Errorf("got %+v err=%v, want the resolved enrichment without a cache", e, err)
	}
}

func TestDeezerEnrichmentService_SymbolOnlyNamesRunUncached(t *testing.T) {
	enricher := &searchtest.DeezerEnricher{ResolvedID: "1", Enrichment: sampleDeezerTrackEnrichment()}
	cache := searchtest.NewNameKeyedCache[domain.DeezerEnrichment]()
	svc := searchtest.NewDeezerEnrichment(t, enricher, cache)

	for range 2 {
		if _, err := svc.Execute(context.Background(), domain.ResultKindTrack, "!!!", "!!!"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	if enricher.Resolves != 2 {
		t.Errorf("resolves = %d, want 2 (a name that normalizes away is never cached)", enricher.Resolves)
	}
	if cache.Sets() != 0 || cache.Negatives() != 0 {
		t.Errorf("a name that normalizes away must not be written, got sets=%d negs=%d", cache.Sets(), cache.Negatives())
	}
}

func TestDeezerEnrichmentService_CacheKeyDistinguishesSplitsOfTheSameWords(t *testing.T) {
	enricher := &searchtest.DeezerEnricher{ResolvedID: "1", Enrichment: sampleDeezerTrackEnrichment()}
	cache := searchtest.NewNameKeyedCache[domain.DeezerEnrichment]()
	svc := searchtest.NewDeezerEnrichment(t, enricher, cache)

	_, _ = svc.Execute(context.Background(), domain.ResultKindTrack, "C", "A B")
	_, _ = svc.Execute(context.Background(), domain.ResultKindTrack, "B C", "A")

	if enricher.Resolves != 2 || cache.Len() != 2 {
		t.Errorf("resolves = %d, cached entries = %d, want 2 and 2 (distinct artist/title splits must not share a key)", enricher.Resolves, cache.Len())
	}
}

func TestDeezerEnrichmentService_CacheKeyPartitionsByKind(t *testing.T) {
	enricher := &searchtest.DeezerEnricher{ResolvedID: "1", Enrichment: sampleDeezerTrackEnrichment()}
	cache := searchtest.NewNameKeyedCache[domain.DeezerEnrichment]()
	svc := searchtest.NewDeezerEnrichment(t, enricher, cache)

	_, _ = svc.Execute(context.Background(), domain.ResultKindTrack, "One More Time", "Daft Punk")
	_, _ = svc.Execute(context.Background(), domain.ResultKindAlbum, "One More Time", "Daft Punk")

	if enricher.Resolves != 2 || cache.Len() != 2 {
		t.Errorf("resolves = %d, cached entries = %d, want 2 and 2 (distinct kinds must not share a key)", enricher.Resolves, cache.Len())
	}
}
