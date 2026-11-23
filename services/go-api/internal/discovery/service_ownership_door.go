package discovery

import ownership "altune/go-api/internal/discovery/service/ownership"

type (
	OwnableItem                = ownership.OwnableItem
	OwnershipEnrichmentService = ownership.OwnershipEnrichmentService
)

var (
	NewOwnershipEnrichmentService = ownership.NewOwnershipEnrichmentService
	NewOwnershipReader            = ownership.NewOwnershipReader
)
