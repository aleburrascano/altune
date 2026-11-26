package search

import unreleased "altune/go-api/internal/search/internal/service/unreleased"

type (
	UnreleasedConfig   = unreleased.Config
	UnreleasedService  = unreleased.Service
	NoUnreleasedCache  = unreleased.NoCache
	NoUnreleasedGenius = unreleased.NoGenius
)

var NewUnreleasedService = unreleased.NewService
