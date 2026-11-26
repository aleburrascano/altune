package download

import catalogpin "altune/go-api/internal/download/internal/adapters/savedlink"

var (
	NewHTTPPageReader = catalogpin.NewHTTPPageReader
	NewSource         = catalogpin.NewSource
	WithPageReader    = catalogpin.WithPageReader
)

const SavedLinkSourceName = catalogpin.SourceName
