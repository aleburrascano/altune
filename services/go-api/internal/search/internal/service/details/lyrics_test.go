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

func sampleLyrics() domain.DeezerLyrics {
	l := domain.EmptyDeezerLyrics()
	l.Plain = "Hello, it's me"
	l.SyncedLines = []domain.SyncedLyricLine{{Timecode: "[00:12.34]", Line: "Hello, it's me", Milliseconds: 12340, Duration: 2000}}
	l.Writers = []string{"Adele Laurie Blue Adkins"}
	return l
}

func TestLyricsService_TranslatesNames(t *testing.T) {
	provider := &searchtest.LyricsProvider{ResolvedID: "3135556", Lyrics: sampleLyrics()}
	svc := searchtest.NewLyrics(t, provider, searchtest.NewNameKeyedCache[domain.DeezerLyrics]())

	_, err := svc.Execute(context.Background(), "Hello", "Adele")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider.LastArtist != "Adele" || provider.LastTitle != "Hello" {
		t.Errorf("names: got artist=%q title=%q", provider.LastArtist, provider.LastTitle)
	}
}

func TestLyricsService_CacheHitShortCircuits(t *testing.T) {
	provider := &searchtest.LyricsProvider{ResolvedID: "3135556", Lyrics: sampleLyrics()}
	cache := searchtest.NewNameKeyedCache[domain.DeezerLyrics]()
	svc := searchtest.NewLyrics(t, provider, cache)

	_, _ = svc.Execute(context.Background(), "Hello", "Adele")
	if provider.Lookups != 1 || cache.Sets() != 1 {
		t.Fatalf("setup: lookups=%d sets=%d", provider.Lookups, cache.Sets())
	}
	_, _ = svc.Execute(context.Background(), "Hello", "Adele")
	if provider.Resolves != 1 || provider.Lookups != 1 {
		t.Errorf("expected cache hit (no extra resolve/lookup), got resolves=%d lookups=%d",
			provider.Resolves, provider.Lookups)
	}
}

func TestLyricsService_UnresolvedCachedNegative(t *testing.T) {
	provider := &searchtest.LyricsProvider{ResolvedID: ""}
	cache := searchtest.NewNameKeyedCache[domain.DeezerLyrics]()
	svc := searchtest.NewLyrics(t, provider, cache)

	l, err := svc.Execute(context.Background(), "Nonexistent", "Nobody")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !l.IsZero() {
		t.Errorf("expected empty lyrics, got %+v", l)
	}
	if cache.Negatives() != 1 {
		t.Errorf("expected a negative cache write, got negs=%d", cache.Negatives())
	}
	if provider.Lookups != 0 {
		t.Errorf("an unresolved id should not be looked up, got lookups=%d", provider.Lookups)
	}
}

func TestLyricsService_NoLyricsCachedNegative(t *testing.T) {
	provider := &searchtest.LyricsProvider{ResolvedID: "1", Lyrics: domain.EmptyDeezerLyrics()}
	cache := searchtest.NewNameKeyedCache[domain.DeezerLyrics]()
	svc := searchtest.NewLyrics(t, provider, cache)

	l, err := svc.Execute(context.Background(), "Instrumental", "Someone")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !l.IsZero() {
		t.Errorf("expected empty lyrics, got %+v", l)
	}
	if cache.Negatives() != 1 {
		t.Errorf("a track with no lyrics should be negative-cached, got negs=%d", cache.Negatives())
	}
}

func TestLyricsService_LookupErrorDegradesWithoutPoisoning(t *testing.T) {
	provider := &searchtest.LyricsProvider{ResolvedID: "1", LookupErr: errors.New("pipe 500")}
	cache := searchtest.NewNameKeyedCache[domain.DeezerLyrics]()
	svc := searchtest.NewLyrics(t, provider, cache)

	l, err := svc.Execute(context.Background(), "Hello", "Adele")
	if !errors.Is(err, details.ErrDegraded) {
		t.Fatalf("a fetch error must surface as ErrDegraded (distinct from no data), got %v", err)
	}
	if !l.IsZero() {
		t.Errorf("expected empty lyrics on error, got %+v", l)
	}
	if cache.Negatives() != 0 {
		t.Errorf("a transient lookup error must not poison the cache, got negs=%d", cache.Negatives())
	}
}

func TestLyricsService_RefusesAMissingCollaborator(t *testing.T) {
	provider := &searchtest.LyricsProvider{}
	cache := searchtest.NewNameKeyedCache[domain.DeezerLyrics]()
	tests := []struct {
		name     string
		provider ports.LyricsProvider
		cache    ports.LyricsCache
		missing  string
	}{
		{"provider", nil, cache, "lyrics provider"},
		{"cache", provider, nil, "lyrics cache"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, err := details.NewLyricsService(tt.provider, tt.cache)

			if svc != nil || !errors.Is(err, details.ErrCollaboratorRequired) || !strings.Contains(err.Error(), tt.missing) {
				t.Fatalf("got (%v, %v), want a refusal naming %q", svc, err, tt.missing)
			}
		})
	}
}
