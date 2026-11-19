package catalog

import handler "altune/go-api/internal/catalog/adapters/handler"

type (
	AudioRateLimit  = handler.AudioRateLimit
	AudioURLHandler = handler.AudioURLHandler
	LibraryHandler  = handler.LibraryHandler
	PlaylistHandler = handler.PlaylistHandler
	StreamHandler   = handler.StreamHandler
	TrackHandler    = handler.TrackHandler
)

var (
	NewAudioURLHandler            = handler.NewAudioURLHandler
	NewFeaturedArtistHandler      = handler.NewFeaturedArtistHandler
	NewLibraryHandler             = handler.NewLibraryHandler
	NewPlaylistHandler            = handler.NewPlaylistHandler
	NewStreamHandler              = handler.NewStreamHandler
	NewTrackHandler               = handler.NewTrackHandler
	WithAudioURLLimiterClock      = handler.WithAudioURLLimiterClock
	WithAudioURLRateLimit         = handler.WithAudioURLRateLimit
	WithPlaylistWriteLimiterClock = handler.WithPlaylistWriteLimiterClock
	WithPlaylistWriteRateLimit    = handler.WithPlaylistWriteRateLimit
	WithPrefetchEnabled           = handler.WithPrefetchEnabled
	WithStreamLimiterClock        = handler.WithStreamLimiterClock
	WithStreamRateLimit           = handler.WithStreamRateLimit
	WithStreamWriteIdleTimeout    = handler.WithStreamWriteIdleTimeout
	WithTrackWriteLimiterClock    = handler.WithTrackWriteLimiterClock
	WithTrackWriteRateLimit       = handler.WithTrackWriteRateLimit
)
