package search

import content "altune/go-api/internal/search/service/artistpage"

type (
	ArtistContentOption     = content.ArtistContentOption
	GetAlbumTracksService   = content.GetAlbumTracksService
	GetArtistContentService = content.GetArtistContentService
)

var (
	NewFeaturedArtistResolver  = content.NewFeaturedArtistResolver
	NewGetAlbumTracksService   = content.NewGetAlbumTracksService
	NewGetArtistContentService = content.NewGetArtistContentService
	WithAlbumCircuitBreaker    = content.WithAlbumCircuitBreaker
	WithAlbumFallbackSearcher  = content.WithAlbumFallbackSearcher
	WithContentCircuitBreaker  = content.WithContentCircuitBreaker
	WithContentEventStore      = content.WithContentEventStore
	WithContentIdentityStore   = content.WithContentIdentityStore
	WithMBAnchor               = content.WithMBAnchor
	WithTrackFeatured          = content.WithTrackFeatured
)
