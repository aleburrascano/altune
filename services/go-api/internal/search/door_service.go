package search

import (
	domain "altune/go-api/internal/search/domain"
	memory "altune/go-api/internal/search/internal/adapters/memory"
	ports "altune/go-api/internal/search/internal/core/ports"
	content "altune/go-api/internal/search/service/artistpage"
	providercall "altune/go-api/internal/search/service/providerguard"
	unreleased "altune/go-api/internal/search/service/unreleased"
)

type (
	ArtistCatalogTitles          = memory.ArtistCatalogTitles
	CountingAlbumTracksProvider  = memory.CountingAlbumTracksProvider
	CountingProvider             = memory.CountingProvider
	CountingRelationshipQuerier  = memory.CountingRelationshipQuerier
	CtxRecordingVocab            = memory.CtxRecordingVocab
	FakeAlbumContentProvider     = memory.AlbumContentProvider
	FakeArtistContentProvider    = memory.ArtistContentProvider
	FakeArtworkCache             = memory.FakeArtworkCache
	FakeArtworkLookup            = memory.ArtworkLookup
	FakeDeezerEnricher           = memory.DeezerEnricher
	FakeEventStore               = memory.FakeEventStore
	FakeFavoritesRepo            = memory.FakeFavoritesRepo
	FakeGeniusSongCatalog        = memory.GeniusSongCatalog
	FakeHistoryEraser            = memory.FakeHistoryEraser
	FakeHistoryReader            = memory.FakeHistoryReader
	FakeHistoryWriter            = memory.FakeHistoryWriter
	FakeIdentityStore            = memory.FakeIdentityStore
	FakeLyricsProvider           = memory.LyricsProvider
	FakeProvider                 = memory.FakeProvider
	FakeRelatedProvider          = memory.FakeRelatedProvider
	FakeResultCache              = memory.FakeResultCache
	FakeUnreleasedStore          = memory.UnreleasedStore
	FakeVocabularyStore          = memory.FakeVocabularyStore
	PanickingEventStore          = memory.PanickingEventStore
	QueryFakeProvider            = memory.QueryFakeProvider
	RecordingActivityFeed        = memory.RecordingActivityFeed
	UpstreamStatusError          = memory.UpstreamStatusError
	StoredArtist                 = memory.StoredArtist
	TrackerSongs                 = memory.TrackerSongs
	AlbumTracksRequest           = content.AlbumTracksRequest
	CircuitState                 = providercall.CircuitState
	Favorite                     = domain.Favorite
	MusicBrainzDiscographyAnchor = ports.MusicBrainzDiscographyAnchor
	ProviderSearchResponse       = domain.ProviderSearchResponse
	RelatedTrackMatch            = ports.RelatedTrackMatch
	UnreleasedArtistKey          = ports.UnreleasedArtistKey
	UnreleasedLookup             = unreleased.Lookup
)

var (
	ErrFavoritesFull  = ports.ErrFavoritesFull
	ErrTrackerPartial = ports.ErrTrackerPartial
)

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
	VocabKindAlbum               = domain.VocabKindAlbum
	VocabKindQuery               = domain.VocabKindQuery
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
	ErrUpstreamDown       = memory.ErrUpstreamDown
	V2Album               = memory.V2Album
	WithISRC              = memory.WithISRC
	WithPop               = memory.WithPop
)
