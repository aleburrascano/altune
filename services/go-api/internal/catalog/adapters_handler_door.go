package catalog

import handler "altune/go-api/internal/catalog/adapters/handler"

type (
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
	WithPlaylistWriteLimiterClock = handler.WithPlaylistWriteLimiterClock
	WithPrefetchEnabled           = handler.WithPrefetchEnabled
	WithStreamLimiterClock        = handler.WithStreamLimiterClock
	WithStreamWriteIdleTimeout    = handler.WithStreamWriteIdleTimeout
	WithTrackWriteLimiterClock    = handler.WithTrackWriteLimiterClock
)
