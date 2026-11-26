package download

import catalogpin "altune/go-api/internal/download/internal/adapters/savedlink"

var (
	NewHTTPPageReader = catalogpin.NewHTTPPageReader
	MustNewSource     = catalogpin.MustNewSource
)

const SavedLinkSourceName = catalogpin.SourceName
