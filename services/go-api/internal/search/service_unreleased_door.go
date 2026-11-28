package search

import "altune/go-api/internal/search/internal/service"

type (
	UnreleasedConfig   = service.UnreleasedConfig
	UnreleasedService  = service.UnreleasedService
	NoUnreleasedCache  = service.NoUnreleasedCache
	NoUnreleasedGenius = service.NoGenius
)

var NewUnreleasedService = service.NewUnreleasedService
