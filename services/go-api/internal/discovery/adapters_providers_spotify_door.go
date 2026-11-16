package discovery

import spotify "altune/go-api/internal/discovery/internal/adapters/providers/spotify"

type SpotifyAdapter = spotify.SpotifyAdapter

var (
	NewSpotifyAdapter         = spotify.NewSpotifyAdapter
	NewSpotifyArtworkResolver = spotify.NewSpotifyArtworkResolver
)
