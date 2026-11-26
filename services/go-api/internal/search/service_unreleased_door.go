package search

import unreleased "altune/go-api/internal/search/service/unreleased"

type (
	UnreleasedConfig  = unreleased.Config
	UnreleasedService = unreleased.Service
)

var (
	NewUnreleasedService    = unreleased.NewService
	WithUnreleasedLifecycle = unreleased.WithLifecycle
	WithUnreleasedStore     = unreleased.WithStore
	WithUnreleasedTrackers  = unreleased.WithTrackers
)

type ArtistToRefresh = unreleased.ArtistToRefresh
