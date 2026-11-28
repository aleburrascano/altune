package search

import (
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/ports"
)

type (
	ChartProvider              = ports.ChartProvider
	DeletedUserDataEraser      = ports.DeletedUserDataEraser
	DiscographyCase            = ports.DiscographyCase
	DiscographyGroupBy         = ports.DiscographyGroupBy
	DiscographyQualityReader   = ports.DiscographyQualityReader
	DiscographySuspectRate     = ports.DiscographySuspectRate
	ISRCRecording              = ports.ISRCRecording
	MusicBrainzRecording       = ports.MusicBrainzRecording
	MusicBrainzRecordingDetail = ports.MusicBrainzRecordingDetail
	MusicBrainzRelease         = ports.MusicBrainzRelease
	MusicBrainzWorkRecording   = ports.MusicBrainzWorkRecording
	QueryCount                 = ports.QueryCount
	SearchProvider             = ports.SearchProvider
	UnreleasedDueArtist        = ports.UnreleasedDueArtist
	SpellingHintsStore         = ports.SpellingHintsStore
)

var ParseDiscographyGroupBy = ports.ParseDiscographyGroupBy

var ErrIdentityStoreUnavailable = domain.ErrIdentityStoreUnavailable
