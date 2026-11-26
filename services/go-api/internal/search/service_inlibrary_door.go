package search

import ownership "altune/go-api/internal/search/service/inlibrary"

type (
	OwnableItem                = ownership.OwnableItem
	OwnershipEnrichmentService = ownership.OwnershipEnrichmentService
	AbsentTrackNumberFiller    = ownership.AbsentTrackNumberFiller
)

var (
	ErrOwnershipCollaboratorRequired = ownership.ErrCollaboratorRequired
	NewOwnershipEnrichmentService    = ownership.NewOwnershipEnrichmentService
	NewOwnershipReader               = ownership.NewOwnershipReader
)
