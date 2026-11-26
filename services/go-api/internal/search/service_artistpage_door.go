package search

import content "altune/go-api/internal/search/internal/service/artistpage"

type (
	AlbumFallback           = content.AlbumFallback
	Anchoring               = content.Anchoring
	ArtistIdentity          = content.ArtistIdentity
	DeezerFeaturedLookup    = content.DeezerFeaturedLookup
	DiscographyObserver     = content.DiscographyObserver
	FeaturedEnrichment      = content.FeaturedEnrichment
	GetAlbumTracksService   = content.GetAlbumTracksService
	GetArtistContentService = content.GetArtistContentService
	MBFeaturedSearcher      = content.MBFeaturedSearcher
	NoAlbumFallback         = content.NoAlbumFallback
	NoAnchoring             = content.NoAnchoring
	NoDeezerFeatured        = content.NoDeezerFeatured
	NoDiscographyObserver   = content.NoDiscographyObserver
	NoFeaturedEnrichment    = content.NoFeaturedEnrichment
	NoMBFeatured            = content.NoMBFeatured
	SeedPathIdentity        = content.SeedPathIdentity
)

var (
	ErrArtistPageCollaboratorRequired = content.ErrCollaboratorRequired

	NewDeezerFeaturedEnrichment = content.NewDeezerFeaturedEnrichment
	NewDiscographyTelemetry     = content.NewDiscographyTelemetry
	NewFeaturedArtistResolver   = content.NewFeaturedArtistResolver
	NewGetAlbumTracksService    = content.NewGetAlbumTracksService
	NewGetArtistContentService  = content.NewGetArtistContentService
	NewMusicBrainzAnchoring     = content.NewMusicBrainzAnchoring
	NewSearchAlbumFallback      = content.NewSearchAlbumFallback
	NewStoreIdentity            = content.NewStoreIdentity
)
