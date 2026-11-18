package discovery

import (
	domain "altune/go-api/internal/discovery/domain"
	memory "altune/go-api/internal/discovery/internal/adapters/memory"
	ports "altune/go-api/internal/discovery/internal/core/ports"
	content "altune/go-api/internal/discovery/service/content"
	providercall "altune/go-api/internal/discovery/service/providercall"
	unreleased "altune/go-api/internal/discovery/service/unreleased"
)

type (
	ArtistCatalogTitles         = memory.ArtistCatalogTitles
	CountingAlbumTracksProvider = memory.CountingAlbumTracksProvider
	CountingProvider            = memory.CountingProvider
	CountingRelationshipQuerier = memory.CountingRelationshipQuerier
	CtxRecordingVocab           = memory.CtxRecordingVocab
	FakeAlbumContentProvider    = memory.AlbumContentProvider
	FakeArtistContentProvider   = memory.ArtistContentProvider
	FakeArtworkCache            = memory.FakeArtworkCache
	FakeArtworkResolver         = memory.ArtworkResolver
	FakeEventStore              = memory.FakeEventStore
	FakeFavoritesRepo           = memory.FakeFavoritesRepo
	FakeGeniusSongCatalog       = memory.GeniusSongCatalog
	FakeHistoryEraser           = memory.FakeHistoryEraser
	FakeHistoryReader           = memory.FakeHistoryReader
	FakeHistoryWriter           = memory.FakeHistoryWriter
	FakeIdentityStore           = memory.FakeIdentityStore
	FakeProvider                = memory.FakeProvider
	FakeRelatedProvider         = memory.FakeRelatedProvider
	FakeResultCache             = memory.FakeResultCache
	FakeUnreleasedStore         = memory.UnreleasedStore
	FakeVocabularyStore         = memory.FakeVocabularyStore
	PanickingEventStore         = memory.PanickingEventStore
	QueryFakeProvider           = memory.QueryFakeProvider
	RecordingActivityFeed       = memory.RecordingActivityFeed
	StatusErr                   = memory.StatusErr
	StoredArtist                = memory.StoredArtist
	TrackerSongs                = memory.TrackerSongs
	AlbumTracksOption           = content.AlbumTracksOption
	AlbumTracksRequest          = content.AlbumTracksRequest
	CircuitState                = providercall.CircuitState
	Favorite                    = domain.Favorite
	MBDiscographyAnchor         = ports.MBDiscographyAnchor
	ProviderSearchResponse      = domain.ProviderSearchResponse
	RelatedTrackMatch           = ports.RelatedTrackMatch
	UnreleasedArtistKey         = ports.UnreleasedArtistKey
	UnreleasedLookup            = unreleased.Lookup
)

var ErrTrackerPartial = ports.ErrTrackerPartial

const (
	ArtworkPathPending           = domain.ArtworkPathPending
	AudioProviderYouTube         = domain.AudioProviderYouTube
	CircuitClosed                = providercall.CircuitClosed
	CircuitHalfOpen              = providercall.CircuitHalfOpen
	CircuitOpen                  = providercall.CircuitOpen
	EventTypeDiscographyObserved = domain.EventTypeDiscographyObserved
	ProviderStatusCircuitOpen    = domain.ProviderStatusCircuitOpen
	ProviderStatusPending        = domain.ProviderStatusPending
	ProviderStatusRateLimited    = domain.ProviderStatusRateLimited
	ProviderStatusTimeout        = domain.ProviderStatusTimeout
	ProviderUnknown              = domain.ProviderUnknown
	RelationshipLibraryMatches   = domain.RelationshipLibraryMatches
)

var (
	AlbumSearchResult     = memory.AlbumSearchResult
	AlbumTitles           = memory.AlbumTitles
	CaptureDetachedLogs   = memory.CaptureDetachedLogs
	CaptureLogs           = memory.CaptureLogs
	CaptureProductionLogs = memory.CaptureProductionLogs
	DeezerAlbum           = memory.DeezerAlbum
	DeezerTrack           = memory.DeezerTrack
	FavoriteOf            = memory.FavoriteOf
	LoggedEvent           = memory.LoggedEvent
	NewFakeResultCache    = memory.NewFakeResultCache
	NewQuery              = memory.NewQuery
	NewUser               = memory.NewUser
	OnlyRecord            = memory.OnlyRecord
	Subtitles             = memory.Subtitles
	Titles                = memory.Titles
	Track                 = memory.Track
	TrackFrom             = memory.TrackFrom
	TrackResult           = memory.TrackResult
	UpstreamDown          = memory.UpstreamDown
	V2Album               = memory.V2Album
	WithISRC              = memory.WithISRC
	WithMBID              = memory.WithMBID
	WithPop               = memory.WithPop
)
