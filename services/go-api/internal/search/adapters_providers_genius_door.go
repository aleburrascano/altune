package search

import genius "altune/go-api/internal/search/internal/adapters/providers/genius"

var (
	NewGeniusArtworkLookup = genius.NewGeniusArtworkLookup
	NewSongCatalog         = genius.NewSongCatalog
)

type GeniusArtworkLookup = genius.GeniusArtworkLookup
