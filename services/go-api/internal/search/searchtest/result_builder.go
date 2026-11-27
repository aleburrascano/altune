package searchtest

import "altune/go-api/internal/search/internal/adapters/memory"

var (
	NewResult         = memory.NewResult
	WithAlbum         = memory.WithAlbum
	WithDuration      = memory.WithDuration
	WithISRCCode      = memory.WithISRCCode
	WithRecordingMBID = memory.WithRecordingMBID
	WithSources       = memory.WithSources
	WithSubtitle      = memory.WithSubtitle
)
