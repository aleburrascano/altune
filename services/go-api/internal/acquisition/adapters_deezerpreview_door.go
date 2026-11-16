package acquisition

import deezerpreview "altune/go-api/internal/acquisition/internal/adapters/deezerpreview"

type (
	PreviewClip         = deezerpreview.Clip
	RawPrinter          = deezerpreview.RawPrinter
	DeezerPreviewTarget = deezerpreview.Target
)

var (
	BestAlignment         = deezerpreview.BestAlignment
	Incomparable          = deezerpreview.Incomparable
	NewDeezerPreview      = deezerpreview.New
	NewFpcalcPrinter      = deezerpreview.NewFpcalcPrinter
	NewPreviewMatcher     = deezerpreview.NewMatcher
	PreviewOverlapSeconds = deezerpreview.OverlapSeconds
)

var (
	ErrDeezerThrottled    = deezerpreview.ErrDeezerThrottled
	ErrFileReachesCap     = deezerpreview.ErrFileReachesCap
	ErrPreviewTooShort    = deezerpreview.ErrPreviewTooShort
	ErrPrintLongerThanRef = deezerpreview.ErrPrintLongerThanRef
)

const (
	DeezerPreviewBaseURL        = deezerpreview.BaseURL
	DefaultMatchMaxBitErrorRate = deezerpreview.DefaultMatchMaxBitErrorRate
	FingerprintedItemCap        = deezerpreview.FingerprintedItemCap
	MinPreviewItems             = deezerpreview.MinPreviewItems
)
