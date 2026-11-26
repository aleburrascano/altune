package search

import ownership "altune/go-api/internal/search/service/inlibrary"

type (
	OwnableItem                = ownership.OwnableItem
	OwnershipEnrichmentService = ownership.OwnershipEnrichmentService
)

var (
	NewOwnershipEnrichmentService = ownership.NewOwnershipEnrichmentService
	NewOwnershipReader            = ownership.NewOwnershipReader
)
