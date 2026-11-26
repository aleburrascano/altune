package search

import (
	handler "altune/go-api/internal/search/adapters/handler"
	domain "altune/go-api/internal/search/domain"
	providermetrics "altune/go-api/internal/search/internal/adapters/providermetrics"
	artworkchain "altune/go-api/internal/search/internal/adapters/providers/artworkfallback"
	deezer "altune/go-api/internal/search/internal/adapters/providers/deezer"
	itunes "altune/go-api/internal/search/internal/adapters/providers/itunes"
	providerhttp "altune/go-api/internal/search/internal/adapters/providers/providerrequests"
	providersearch "altune/go-api/internal/search/internal/adapters/providers/providersearch"
	soundcloud "altune/go-api/internal/search/internal/adapters/providers/soundcloud"
	enrich "altune/go-api/internal/search/internal/core/details"
	ports "altune/go-api/internal/search/internal/core/ports"
	rank "altune/go-api/internal/search/service/rank"
	unreleased "altune/go-api/internal/search/service/unreleased"
)

type (
	ContentFetchResponseDTO          = handler.ContentFetchResponseDTO
	Contributor                      = domain.Contributor
	DeezerEnrichment                 = domain.DeezerEnrichment
	DeezerEnrichmentResponseDTO      = handler.DeezerEnrichmentResponseDTO
	DeezerLyrics                     = domain.DeezerLyrics
	DiscoveryRateLimits              = handler.DiscoveryRateLimits
	DiscoverySearchResponse          = handler.DiscoverySearchResponse
	EnrichmentResponseDTO            = handler.EnrichmentResponseDTO
	EventType                        = domain.EventType
	FeaturedArtist                   = domain.FeaturedArtist
	LyricsResponseDTO                = handler.LyricsResponseDTO
	OwnedTrack                       = ports.OwnedTrack
	OwnedTrackLister                 = ports.OwnedTrackLister
	OwnershipReader                  = ports.OwnershipReader
	PartialKindsError                = domain.PartialKindsError
	PartialResultError               = domain.PartialResultError
	ProviderMetricsCountingTransport = providermetrics.CountingTransport
	ProviderMetricsOutcomes          = providermetrics.Outcomes
	RecordType                       = domain.RecordType
	RequestLimit                     = handler.RequestLimit
	SearchHistoryItemDTO             = handler.SearchHistoryItemDTO
	SearchResultDTO                  = handler.SearchResultDTO
	SuggestResponse                  = handler.SuggestResponse
	SyncedLyricLine                  = domain.SyncedLyricLine
	TrackNumberFiller                = ports.TrackNumberFiller
	TrackPosition                    = domain.TrackPosition
	UnreleasedArtistDTO              = handler.UnreleasedArtistDTO
	UnreleasedHit                    = ports.UnreleasedHit
	UnreleasedResponseDTO            = handler.UnreleasedResponseDTO
	UnreleasedSearchResponseDTO      = handler.UnreleasedSearchResponseDTO
	UnreleasedSongDTO                = handler.UnreleasedSongDTO
)

const (
	ConfidenceHigh                = domain.ConfidenceHigh
	ContributorFeatured           = domain.ContributorFeatured
	ContributorMain               = domain.ContributorMain
	DeezerPlaceholderImage        = deezer.DeezerPlaceholderImage
	EntityResolutionISRC          = domain.EntityResolutionISRC
	EventTypeAuthFailed           = domain.EventTypeAuthFailed
	EventTypeDetailHealth         = domain.EventTypeDetailHealth
	EventTypeFailureShown         = domain.EventTypeFailureShown
	EventTypePlaybackHealth       = domain.EventTypePlaybackHealth
	EventTypeResultsShown         = domain.EventTypeResultsShown
	EventTypeSearchDegraded       = domain.EventTypeSearchDegraded
	EventTypeSearchFailed         = domain.EventTypeSearchFailed
	EventTypeUserAction           = domain.EventTypeUserAction
	ExtraOwnedTrackID             = domain.ExtraOwnedTrackID
	MaxSearchQueryRunes           = domain.MaxSearchQueryRunes
	MaxSearchQueryTokens          = domain.MaxSearchQueryTokens
	ProviderHTTPBodyCap           = providerhttp.BodyCap
	ProviderHTTPQueueDepth        = providerhttp.ProviderQueueDepth
	ProviderKeyCoverArtArchive    = domain.ProviderKeyCoverArtArchive
	RecordTypeAlbum               = domain.RecordTypeAlbum
	RecordTypeCompilation         = domain.RecordTypeCompilation
	RecordTypeEP                  = domain.RecordTypeEP
	RecordTypeSingle              = domain.RecordTypeSingle
	RecordTypeUnknown             = domain.RecordTypeUnknown
	UnreleasedCurrentBuildVersion = unreleased.CurrentBuildVersion
)

var (
	BoolPtr                           = domain.BoolPtr
	DefaultDiscoveryRateLimits        = handler.DefaultDiscoveryRateLimits
	EmptyDeezerEnrichment             = domain.EmptyDeezerEnrichment
	EmptyDeezerLyrics                 = domain.EmptyDeezerLyrics
	ErrEnrichDegraded                 = enrich.ErrDegraded
	ErrProviderRateLimitQueueTimeout  = ports.ErrProviderRateLimitQueueTimeout
	ErrSoundCloudYtdlpBusy            = soundcloud.ErrYtdlpBusy
	ITunesStripAlbumTypeSuffix        = itunes.StripAlbumTypeSuffix
	IsUnverifiedArtworkMiss           = ports.IsUnverifiedArtworkMiss
	NewChainedArtworkResolverWithin   = artworkchain.NewChainedArtworkResolverWithin
	NewProviderHTTPHeadRequest        = providerhttp.NewHeadRequest
	NewProviderHTTPMinIntervalLimiter = providerhttp.NewMinIntervalLimiter
	NewProviderHTTPRateLimiter        = providerhttp.NewRateLimiter
	NewProviderResult                 = domain.NewProviderResult
	NewSoundCloudAdapterWithRunner    = soundcloud.NewSoundCloudAdapterWithRunner
	OwnershipISRCKey                  = domain.OwnershipISRCKey
	OwnershipKey                      = domain.OwnershipKey
	ProviderHTTPArtworkFailure        = providerhttp.ArtworkFailure
	ProviderHTTPGetBytes              = providerhttp.GetBytes
	ProviderHTTPGetBytesCapped        = providerhttp.GetBytesCapped
	ProviderHTTPGetJSON               = providerhttp.GetJSON
	ProviderHTTPGetJSONWithStatus     = providerhttp.GetJSONWithStatus
	ProviderHTTPGetText               = providerhttp.GetText
	ProviderHTTPPostBytesCapped       = providerhttp.PostBytesCapped
	ProviderHTTPPostBytesCappedOK     = providerhttp.PostBytesCappedOK
	ProviderHTTPPostJSON              = providerhttp.PostJSON
	ProviderSearchAllKinds            = providersearch.AllSearchKinds
	ProviderSearchDslQuote            = providersearch.DslQuote
	ProviderSearchMbLuceneEscape      = providersearch.MbLuceneEscape
	ProviderSearchParseListeners      = providersearch.ParseListeners
	ProviderSearchAcrossKinds         = providersearch.SearchAcrossKinds
	RankCollapseArtistDuplicates      = rank.CollapseArtistDuplicates
	RankMerge                         = rank.Merge
	ResultSignature                   = domain.ResultSignature
	StampResolutionTier               = domain.StampResolutionTier
	WithProviderHTTPHeader            = providerhttp.WithHeader
)
