package search

import "altune/go-api/internal/search/internal/service"

type (
	OwnershipEnrichmentService = service.OwnershipEnrichmentService
)

var (
	NewOwnershipEnrichmentService = service.NewOwnershipEnrichmentService
	NewOwnershipReader            = service.NewOwnershipReader
)
