package search

import enrich "altune/go-api/internal/search/internal/core/details"

type (
	DetailsService    = enrich.DetailsService
	NoArtwork         = enrich.NoArtwork
	NoEnrichmentCache = enrich.NoEnrichmentCache
	NoMBIDIndex       = enrich.NoMBIDIndex

	NoNameKeyedCache[T any] = enrich.NoNameKeyedCache[T]
)

var (
	ErrEnrichmentCollaboratorRequired = enrich.ErrCollaboratorRequired

	NewDeezerEnrichmentService = enrich.NewDeezerEnrichmentService
	NewEnrichmentService       = enrich.NewEnrichmentService
	NewLyricsService           = enrich.NewLyricsService
)
