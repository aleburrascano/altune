package discovery

import (
	handles "altune/go-api/internal/discovery/internal/adapters/handles"
	compose "altune/go-api/internal/discovery/internal/shell/compose"
)

type (
	Deps               = handles.Deps
	ProviderSettings   = handles.ProviderSettings
	SearchSettings     = handles.SearchSettings
	UnreleasedSettings = handles.UnreleasedSettings
	Discovery          = compose.Discovery
)

var New = compose.New
