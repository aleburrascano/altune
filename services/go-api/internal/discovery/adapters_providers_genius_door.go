package discovery

import genius "altune/go-api/internal/discovery/internal/adapters/providers/genius"

var (
	NewGeniusArtworkResolver = genius.NewGeniusArtworkResolver
	NewSongCatalog           = genius.NewSongCatalog
)

type GeniusArtworkResolver = genius.GeniusArtworkResolver
