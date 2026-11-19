package discovery

import enrich "altune/go-api/internal/discovery/internal/core/enrich"

type EnrichmentService = enrich.EnrichmentService

var (
	NewDeezerEnrichmentService = enrich.NewDeezerEnrichmentService
	NewEnrichmentService       = enrich.NewEnrichmentService
	NewLyricsService           = enrich.NewLyricsService
)
