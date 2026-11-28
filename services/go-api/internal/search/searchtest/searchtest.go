package searchtest

import (
	"altune/go-api/internal/search/internal/adapters/memory"
	"altune/go-api/internal/search/internal/adapters/telemetry"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/ports"
	"altune/go-api/internal/search/internal/service"
)

type (
	ArtistCatalogTitles     = memory.ArtistCatalogTitles
	Collaborators           = service.Collaborators
	DiscographyCase         = ports.DiscographyCase
	DiscographyGroupBy      = ports.DiscographyGroupBy
	DiscographySuspectRate  = ports.DiscographySuspectRate
	FakeProvider            = memory.FakeProvider
	InteractionEvent        = domain.InteractionEvent
	NoMBFeatured            = service.NoMBFeatured
	NoUnreleasedCache       = service.NoUnreleasedCache
	NoUnreleasedGenius      = service.NoGenius
	ProviderLink            = domain.ProviderLink
	ProviderMetricsSnapshot = telemetry.Snapshot
	SearchProvider          = ports.SearchProvider
	SmokeQueryResult        = service.SmokeQueryResult
	UnreleasedDueArtist     = ports.UnreleasedDueArtist
	UnreleasedStore         = memory.UnreleasedStore
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
	MustUnreleased       = memory.MustUnreleased
	NewCircuitBreaker    = service.NewCircuitBreaker
	NewFeaturedResolver  = memory.NewFeaturedResolver
	NewResult            = memory.NewResult
	NewSearchService     = memory.NewSearchService
	NewSearchServiceWith = memory.NewSearchServiceWith
	NewTracker           = memory.NewTracker
	NewUnreleasedService = service.NewUnreleasedService
	ParseProviderName    = domain.ParseProviderName
	UnreleasedStored     = memory.UnreleasedStored
	UnreleasedWiring     = memory.UnreleasedWiring
	WithAlbum            = memory.WithAlbum
	WithDuration         = memory.WithDuration
	WithExplicit         = memory.WithExplicit
	WithISRCCode         = memory.WithISRCCode
	WithRecordingMBID    = memory.WithRecordingMBID
	WithReleaseMBID      = memory.WithReleaseMBID
	WithSources          = memory.WithSources
	WithSubtitle         = memory.WithSubtitle
	WithUPC              = memory.WithUPC
	WithYear             = memory.WithYear
)
