package memory

import (
	"altune/go-api/internal/search/internal/ports"
	"altune/go-api/internal/search/internal/service"
)

func NewDeezerEnrichment(t TB, enricher ports.DeezerEnricher, cache ports.DeezerEnrichmentCache) *service.DeezerEnrichmentService {
	t.Helper()
	svc, err := service.NewDeezerEnrichmentService(enricher, cache)
	if err != nil {
		t.Fatalf("details.NewDeezerEnrichmentService: %v", err)
	}
	return svc
}

func NewLyrics(t TB, provider ports.LyricsProvider, cache ports.LyricsCache) *service.LyricsService {
	t.Helper()
	svc, err := service.NewLyricsService(provider, cache)
	if err != nil {
		t.Fatalf("details.NewLyricsService: %v", err)
	}
	return svc
}
