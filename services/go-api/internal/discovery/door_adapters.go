package discovery

import (
	domain "altune/go-api/internal/discovery/domain"
	authbridge "altune/go-api/internal/discovery/internal/adapters/authbridge"
	itunes "altune/go-api/internal/discovery/internal/adapters/providers/itunes"
	providerhttp "altune/go-api/internal/discovery/internal/adapters/providers/providerhttp"
	providersearch "altune/go-api/internal/discovery/internal/adapters/providers/providersearch"
	soundcloud "altune/go-api/internal/discovery/internal/adapters/providers/soundcloud"
	ports "altune/go-api/internal/discovery/internal/core/ports"
)

type (
	PartialKindsError = domain.PartialKindsError
)

const (
	ProviderHTTPBodyCap            = providerhttp.BodyCap
	ProviderHTTPProviderQueueDepth = providerhttp.ProviderQueueDepth
	ProviderKeyCoverArtArchive     = domain.ProviderKeyCoverArtArchive
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
	ProviderSearchAllSearchKinds      = providersearch.AllSearchKinds
	ProviderSearchDslQuote            = providersearch.DslQuote
	ProviderSearchMbLuceneEscape      = providersearch.MbLuceneEscape
	ProviderSearchParseListeners      = providersearch.ParseListeners
	ProviderSearchSearchAcrossKinds   = providersearch.SearchAcrossKinds
	WithProviderHTTPHeader            = providerhttp.WithHeader
)
