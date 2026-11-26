package search

import search "altune/go-api/internal/search/service/query"

type (
	SearchOption  = search.Option
	SearchOutput  = search.SearchOutput
	SearchService = search.Service
	Collaborators = search.Collaborators
	ArtworkFill   = search.ArtworkFill
	NoRelated     = search.NoRelated
)

var (
	NewSearchService              = search.NewService
	ErrSearchCollaboratorRequired = search.ErrCollaboratorRequired
	ErrSearchInvalidOption        = search.ErrInvalidOption
	AbsentCollaborators           = search.AbsentCollaborators
	WithCrossKindProminence       = search.WithCrossKindProminence
	WithExploration               = search.WithExploration
	WithSearchDeadline            = search.WithSearchDeadline
	WithTailDemotion              = search.WithTailDemotion
)
