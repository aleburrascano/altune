package discovery

import ports "altune/go-api/internal/discovery/internal/core/ports"

type (
	AlbumContentProvider     = ports.AlbumContentProvider
	ArtistContentProvider    = ports.ArtistContentProvider
	ArtworkResolver          = ports.ArtworkResolver
	BehavioralLabel          = ports.BehavioralLabel
	BehavioralLabelStore     = ports.BehavioralLabelStore
	BehavioralSignal         = ports.BehavioralSignal
	BehavioralSignalStore    = ports.BehavioralSignalStore
	ChartProvider            = ports.ChartProvider
	DeletedIdentityEraser    = ports.DeletedIdentityEraser
	DiscographyCase          = ports.DiscographyCase
	DiscographyGroupBy       = ports.DiscographyGroupBy
	DiscographyQualityReader = ports.DiscographyQualityReader
	DiscographySuspectRate   = ports.DiscographySuspectRate
	EventStore               = ports.EventStore
	GeniusSongCatalog        = ports.GeniusSongCatalog
	ISRCRecording            = ports.ISRCRecording
	IdentityStore            = ports.IdentityStore
	MBRecording              = ports.MBRecording
	MBRecordingDetail        = ports.MBRecordingDetail
	MBRelease                = ports.MBRelease
	MBWorkRecording          = ports.MBWorkRecording
	QueryCount               = ports.QueryCount
	RelatedTracksProvider    = ports.RelatedTracksProvider
	SearchProvider           = ports.SearchProvider
	SearchTimeouter          = ports.SearchTimeouter
	TaggingArtworkResolver   = ports.TaggingArtworkResolver
	UnreleasedCache          = ports.UnreleasedCache
	UnreleasedDueArtist      = ports.UnreleasedDueArtist
	UnreleasedStore          = ports.UnreleasedStore
	VocabularyStore          = ports.VocabularyStore
)

var ParseDiscographyGroupBy = ports.ParseDiscographyGroupBy

var ErrIdentityStoreUnavailable = ports.ErrIdentityStoreUnavailable

const GroupByProvider = ports.GroupByProvider

type ArtworkIdentity = ports.ArtworkIdentity

var (
	ErrArtworkDegraded    = ports.ErrArtworkDegraded
	ErrArtworkUnavailable = ports.ErrArtworkUnavailable
	ErrPartialCatalog     = ports.ErrPartialCatalog
)
