package search

import related "altune/go-api/internal/search/internal/service/related"

var (
	ErrRelatedCollaboratorRequired = related.ErrCollaboratorRequired

	NewGetRelatedTracksService = related.NewGetRelatedTracksService
)
