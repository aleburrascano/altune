package discovery

import enrich "altune/go-api/internal/discovery/service/enrich"

type EnrichmentService = enrich.EnrichmentService

var (
	NewDeezerEnrichmentService = enrich.NewDeezerEnrichmentService
	NewEnrichmentService       = enrich.NewEnrichmentService
	NewLastFmEnrichmentService = enrich.NewLastFmEnrichmentService
	NewLyricsService           = enrich.NewLyricsService
	WithMBIDMemo               = enrich.WithMBIDMemo
)
