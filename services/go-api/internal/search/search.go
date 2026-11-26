package search

import (
	handles "altune/go-api/internal/search/internal/adapters/connections"
	compose "altune/go-api/internal/search/internal/shell/setup"
)

type (
	Deps               = handles.Deps
	ProviderSettings   = handles.ProviderSettings
	SearchSettings     = handles.SearchSettings
	UnreleasedSettings = handles.UnreleasedSettings
	Discovery          = compose.Discovery
)

var New = compose.New
