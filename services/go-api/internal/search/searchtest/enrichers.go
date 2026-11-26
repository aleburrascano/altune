package searchtest

import (
	"altune/go-api/internal/search/internal/adapters/memory"
	"altune/go-api/internal/search/internal/core/details"
	"altune/go-api/internal/search/internal/core/ports"
	"testing"
)

type (
	DeezerEnricher = memory.DeezerEnricher
	LyricsProvider = memory.LyricsProvider
)

func NewDeezerEnrichment(t testing.TB, enricher ports.DeezerEnricher, cache ports.DeezerEnrichmentCache) *details.DeezerEnrichmentService {
	t.Helper()
	svc, err := details.NewDeezerEnrichmentService(enricher, cache)
	if err != nil {
		t.Fatalf("details.NewDeezerEnrichmentService: %v", err)
	}
	return svc
}

func NewLyrics(t testing.TB, provider ports.LyricsProvider, cache ports.LyricsCache) *details.LyricsService {
	t.Helper()
	svc, err := details.NewLyricsService(provider, cache)
	if err != nil {
		t.Fatalf("details.NewLyricsService: %v", err)
	}
	return svc
}
