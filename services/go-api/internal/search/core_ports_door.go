package search

import ports "altune/go-api/internal/search/internal/core/ports"

type (
	AlbumContentProvider       = ports.AlbumContentProvider
	ArtistContentProvider      = ports.ArtistContentProvider
	ArtworkLookup              = ports.ArtworkLookup
	BehavioralSignal           = ports.BehavioralSignal
	ChartProvider              = ports.ChartProvider
	DeletedUserDataEraser      = ports.DeletedUserDataEraser
	DiscographyCase            = ports.DiscographyCase
	DiscographyGroupBy         = ports.DiscographyGroupBy
	DiscographyQualityReader   = ports.DiscographyQualityReader
	DiscographySuspectRate     = ports.DiscographySuspectRate
	GeniusSongCatalog          = ports.GeniusSongCatalog
	ISRCRecording              = ports.ISRCRecording
	IdentityHit                = ports.IdentityHit
	IdentityRef                = ports.IdentityRef
	IdentityStore              = ports.IdentityStore
	MusicBrainzRecording       = ports.MusicBrainzRecording
	MusicBrainzRecordingDetail = ports.MusicBrainzRecordingDetail
	MusicBrainzRelease         = ports.MusicBrainzRelease
	MusicBrainzWorkRecording   = ports.MusicBrainzWorkRecording
	QueryCount                 = ports.QueryCount
	RelatedTracksProvider      = ports.RelatedTracksProvider
	SearchProvider             = ports.SearchProvider
	UnreleasedCache            = ports.UnreleasedCache
	UnreleasedDueArtist        = ports.UnreleasedDueArtist
	UnreleasedStore            = ports.UnreleasedStore
	SpellingHintsStore         = ports.SpellingHintsStore
)

var (
	ParseDiscographyGroupBy      = ports.ParseDiscographyGroupBy
	ErrUnknownDiscographyGroupBy = ports.ErrUnknownDiscographyGroupBy
)

var ErrIdentityStoreUnavailable = ports.ErrIdentityStoreUnavailable

const GroupByProvider = ports.GroupByProvider

type ArtworkIdentity = ports.ArtworkIdentity

var (
	ErrArtworkDegraded    = ports.ErrArtworkDegraded
	ErrArtworkUnavailable = ports.ErrArtworkUnavailable
	ErrPartialCatalog     = ports.ErrPartialCatalog
)
