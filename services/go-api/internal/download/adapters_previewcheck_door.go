package download

import deezerpreview "altune/go-api/internal/download/internal/adapters/previewcheck"

type (
	PreviewClip         = deezerpreview.Clip
	RawFingerprint      = deezerpreview.RawFingerprint
	DeezerPreviewTarget = deezerpreview.Target
)

var (
	BestOffset            = deezerpreview.BestOffset
	NotComparable         = deezerpreview.NotComparable
	NewDeezerPreview      = deezerpreview.New
	MustNewDeezerPreview  = deezerpreview.MustNew
	NewFpcalcPrinter      = deezerpreview.NewFpcalcPrinter
	NewPreviewMatcher     = deezerpreview.NewMatcher
	PreviewOverlapSeconds = deezerpreview.OverlapSeconds
)

var (
	ErrDeezerClockRequired   = deezerpreview.ErrClockRequired
	ErrDeezerSleeperRequired = deezerpreview.ErrSleeperRequired
	ErrDeezerThrottled       = deezerpreview.ErrDeezerThrottled
	ErrFileReachesCap        = deezerpreview.ErrFileReachesCap
	ErrPreviewTooShort       = deezerpreview.ErrPreviewTooShort
	ErrPrintLongerThanRef    = deezerpreview.ErrPrintLongerThanRef
)

const (
	DeezerPreviewBaseURL        = deezerpreview.BaseURL
	DefaultMatchMaxBitErrorRate = deezerpreview.DefaultMatchMaxBitErrorRate
	FingerprintedItemCap        = deezerpreview.FingerprintedItemCap
	MinPreviewItems             = deezerpreview.MinPreviewItems
)
