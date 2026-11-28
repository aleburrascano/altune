package searchtest

import (
	"altune/go-api/internal/search/internal/adapters/memory"
	"altune/go-api/internal/search/internal/ports"
	"altune/go-api/internal/search/internal/service"
	"testing"
)

type (
	DeezerEnricher = memory.DeezerEnricher
	LyricsProvider = memory.LyricsProvider
)

func NewDeezerEnrichment(t testing.TB, enricher ports.DeezerEnricher, cache ports.DeezerEnrichmentCache) *service.DeezerEnrichmentService {
	t.Helper()
	svc, err := service.NewDeezerEnrichmentService(enricher, cache)
	if err != nil {
		t.Fatalf("details.NewDeezerEnrichmentService: %v", err)
	}
	return svc
}

func NewLyrics(t testing.TB, provider ports.LyricsProvider, cache ports.LyricsCache) *service.LyricsService {
	t.Helper()
	svc, err := service.NewLyricsService(provider, cache)
	if err != nil {
		t.Fatalf("details.NewLyricsService: %v", err)
	}
	return svc
}
