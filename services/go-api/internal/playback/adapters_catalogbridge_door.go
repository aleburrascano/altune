package playback

import catalogbridge "altune/go-api/internal/playback/internal/adapters/catalogbridge"

var (
	NewNowPlayingReader   = catalogbridge.NewNowPlayingReader
	WithNowPlayingMetrics = catalogbridge.WithNowPlayingMetrics
)
