package acquisition

import catalogpin "altune/go-api/internal/acquisition/internal/adapters/catalogpin"

var (
	NewHTTPPageReader = catalogpin.NewHTTPPageReader
	NewSource         = catalogpin.NewSource
	WithPageReader    = catalogpin.WithPageReader
)

const CatalogpinSourceName = catalogpin.SourceName
