package download

import deezerpreview "altune/go-api/internal/download/internal/adapters/previewcheck"

var (
	MustNewDeezerPreview = deezerpreview.MustNew
	NewFpcalcPrinter     = deezerpreview.NewFpcalcPrinter
	NewPreviewMatcher    = deezerpreview.NewMatcher
)

const (
	DeezerPreviewBaseURL        = deezerpreview.BaseURL
	DefaultMatchMaxBitErrorRate = deezerpreview.DefaultMatchMaxBitErrorRate
)
