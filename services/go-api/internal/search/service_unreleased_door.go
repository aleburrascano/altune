package search

import unreleased "altune/go-api/internal/search/internal/service/unreleased"

type (
	UnreleasedConfig     = unreleased.Config
	UnreleasedService    = unreleased.Service
	NoUnreleasedCache    = unreleased.NoCache
	NoUnreleasedGenius   = unreleased.NoGenius
	NoUnreleasedTrackers = unreleased.NoTrackers
	UnreleasedWiring     = unreleased.Wiring
	UnreleasedStorage    = unreleased.Storage
)

var (
	ErrUnreleasedCollaboratorRequired = unreleased.ErrCollaboratorRequired
	NewUnreleasedService              = unreleased.NewService
	NewUnreleasedCachedStorage        = unreleased.NewCachedStorage
	NewUnreleasedStoredStorage        = unreleased.NewStoredStorage
)

type ArtistToRefresh = unreleased.ArtistToRefresh
