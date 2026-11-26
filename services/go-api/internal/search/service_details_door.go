package search

import enrich "altune/go-api/internal/search/internal/core/details"

type DetailsService = enrich.DetailsService

var (
	NewDeezerEnrichmentService = enrich.NewDeezerEnrichmentService
	NewEnrichmentService       = enrich.NewEnrichmentService
	NewLyricsService           = enrich.NewLyricsService
)
