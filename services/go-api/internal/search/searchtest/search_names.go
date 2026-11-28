package searchtest

import (
	"altune/go-api/internal/search/internal/adapters/telemetry"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/ports"
	"altune/go-api/internal/search/internal/service"
)

type (
	InteractionEvent        = domain.InteractionEvent
	ProviderLink            = domain.ProviderLink
	DiscographyCase         = ports.DiscographyCase
	DiscographyGroupBy      = ports.DiscographyGroupBy
	DiscographySuspectRate  = ports.DiscographySuspectRate
	SearchProvider          = ports.SearchProvider
	UnreleasedDueArtist     = ports.UnreleasedDueArtist
	Collaborators           = service.Collaborators
	NoMBFeatured            = service.NoMBFeatured
	NoUnreleasedCache       = service.NoUnreleasedCache
	NoUnreleasedGenius      = service.NoGenius
	SmokeQueryResult        = service.SmokeQueryResult
	ProviderMetricsSnapshot = telemetry.Snapshot
)

const (
	ProviderDeezer     = domain.ProviderDeezer
	ProviderSoundCloud = domain.ProviderSoundCloud
	ProviderSpotify    = domain.ProviderSpotify
	ProviderUnknown    = domain.ProviderUnknown
	ProviderYouTube    = domain.ProviderYouTube
)

var (
	AllKinds             = domain.AllKinds
	ParseProviderName    = domain.ParseProviderName
	NewCircuitBreaker    = service.NewCircuitBreaker
	NewUnreleasedService = service.NewUnreleasedService
)
