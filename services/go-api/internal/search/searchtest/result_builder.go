package searchtest

import "altune/go-api/internal/search/internal/adapters/memory"

var (
	NewResult         = memory.NewResult
	WithAlbum         = memory.WithAlbum
	WithDuration      = memory.WithDuration
	WithExplicit      = memory.WithExplicit
	WithISRCCode      = memory.WithISRCCode
	WithRecordingMBID = memory.WithRecordingMBID
	WithReleaseMBID   = memory.WithReleaseMBID
	WithSources       = memory.WithSources
	WithSubtitle      = memory.WithSubtitle
	WithUPC           = memory.WithUPC
	WithYear          = memory.WithYear
)
