package search

import related "altune/go-api/internal/search/service/related"

var (
	ErrRelatedCollaboratorRequired = related.ErrCollaboratorRequired

	NewGetRelatedTracksService = related.NewGetRelatedTracksService
)
