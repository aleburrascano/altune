package discovery

import (
	domain "altune/go-api/internal/discovery/domain"
	authbridge "altune/go-api/internal/discovery/internal/adapters/authbridge"
	providermetrics "altune/go-api/internal/discovery/internal/adapters/providermetrics"
	itunes "altune/go-api/internal/discovery/internal/adapters/providers/itunes"
	providerhttp "altune/go-api/internal/discovery/internal/adapters/providers/providerhttp"
	providersearch "altune/go-api/internal/discovery/internal/adapters/providers/providersearch"
	soundcloud "altune/go-api/internal/discovery/internal/adapters/providers/soundcloud"
	ports "altune/go-api/internal/discovery/internal/core/ports"
)

type (
	Contributor                      = domain.Contributor
	FeaturedArtist                   = domain.FeaturedArtist
	LastFmEnrichment                 = domain.LastFmEnrichment
	PartialKindsError                = domain.PartialKindsError
	PartialResultError               = domain.PartialResultError
	ProviderMetricsCountingTransport = providermetrics.CountingTransport
	ProviderMetricsOutcomes          = providermetrics.Outcomes
	RecordType                       = domain.RecordType
	TrackPosition                    = domain.TrackPosition
)

const (
	ContributorFeatured        = domain.ContributorFeatured
	ContributorMain            = domain.ContributorMain
	ProviderHTTPBodyCap        = providerhttp.BodyCap
	ProviderHTTPQueueDepth     = providerhttp.ProviderQueueDepth
	ProviderKeyCoverArtArchive = domain.ProviderKeyCoverArtArchive
	RecordTypeAlbum            = domain.RecordTypeAlbum
	RecordTypeCompilation      = domain.RecordTypeCompilation
	RecordTypeEP               = domain.RecordTypeEP
	RecordTypeSingle           = domain.RecordTypeSingle
	RecordTypeUnknown          = domain.RecordTypeUnknown
)

var (
	AuthBridgeClientKey               = authbridge.ClientKey
	AuthBridgeRequireUserID           = authbridge.RequireUserID
	AuthBridgeUserIDFromContext       = authbridge.UserIDFromContext
	ErrProviderRateLimitQueueTimeout  = ports.ErrProviderRateLimitQueueTimeout
	ErrSoundCloudYtdlpBusy            = soundcloud.ErrYtdlpBusy
	ITunesStripAlbumTypeSuffix        = itunes.StripAlbumTypeSuffix
	NewProviderHTTPHeadRequest        = providerhttp.NewHeadRequest
	NewProviderHTTPMinIntervalLimiter = providerhttp.NewMinIntervalLimiter
	NewProviderHTTPRateLimiter        = providerhttp.NewRateLimiter
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
	WithProviderHTTPHeader            = providerhttp.WithHeader
)
