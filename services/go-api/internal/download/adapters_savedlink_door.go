package download

import catalogpin "altune/go-api/internal/download/internal/adapters/savedlink"

var (
	NewHTTPPageReader = catalogpin.NewHTTPPageReader
	NewSource         = catalogpin.NewSource
	MustNewSource     = catalogpin.MustNewSource
	NoPageReader      = catalogpin.NoPageReader
)

var (
	ErrSavedLinkFetcherRequired    = catalogpin.ErrFetcherRequired
	ErrSavedLinkPageReaderRequired = catalogpin.ErrPageReaderRequired
)

const SavedLinkSourceName = catalogpin.SourceName
