package discovery

import (
	domain "altune/go-api/internal/discovery/domain"
	ports "altune/go-api/internal/discovery/internal/core/ports"
	content "altune/go-api/internal/discovery/service/content"
	providercall "altune/go-api/internal/discovery/service/providercall"
	unreleased "altune/go-api/internal/discovery/service/unreleased"
)

type (
	AlbumTracksOption      = content.AlbumTracksOption
	AlbumTracksRequest     = content.AlbumTracksRequest
	CircuitState           = providercall.CircuitState
	Favorite               = domain.Favorite
	MBDiscographyAnchor    = ports.MBDiscographyAnchor
	ProviderSearchResponse = domain.ProviderSearchResponse
	RelatedTrackMatch      = ports.RelatedTrackMatch
	UnreleasedArtistKey    = ports.UnreleasedArtistKey
	UnreleasedLookup       = unreleased.Lookup
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
