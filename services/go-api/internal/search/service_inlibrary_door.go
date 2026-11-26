package search

import ownership "altune/go-api/internal/search/internal/service/inlibrary"

type (
	OwnershipEnrichmentService = ownership.OwnershipEnrichmentService
)

var (
	NewOwnershipEnrichmentService = ownership.NewOwnershipEnrichmentService
	NewOwnershipReader            = ownership.NewOwnershipReader
)
