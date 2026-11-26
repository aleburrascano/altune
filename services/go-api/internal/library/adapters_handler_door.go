package library

import handler "altune/go-api/internal/library/internal/adapters/handler"

type (
	AudioURLHandler = handler.AudioURLHandler
	LibraryHandler  = handler.LibraryHandler
	PlaylistHandler = handler.PlaylistHandler
	StreamHandler   = handler.StreamHandler
	TrackHandler    = handler.TrackHandler
)

type (
	AddTrackToPlaylistRequest        = handler.AddTrackToPlaylistRequest
	AddTracksToPlaylistRequest       = handler.AddTracksToPlaylistRequest
	AddTracksToPlaylistResponse      = handler.AddTracksToPlaylistResponse
	CreatePlaylistRequest            = handler.CreatePlaylistRequest
	CreateTrackRequest               = handler.CreateTrackRequest
	ListTracksResponse               = handler.ListTracksResponse
	PlaylistDetailResponse           = handler.PlaylistDetailResponse
	PlaylistResponse                 = handler.PlaylistResponse
	RemoveTracksFromPlaylistRequest  = handler.RemoveTracksFromPlaylistRequest
	RemoveTracksFromPlaylistResponse = handler.RemoveTracksFromPlaylistResponse
	RenamePlaylistRequest            = handler.RenamePlaylistRequest
	ReorderTracksRequest             = handler.ReorderTracksRequest
	SetTrackNumberRequest            = handler.SetTrackNumberRequest
	TrackResponse                    = handler.TrackResponse
)

var (
	ErrHandlerClockRequired    = handler.ErrClockRequired
	MustNewAudioURLHandler     = handler.MustNewAudioURLHandler
	MustNewPlaylistHandler     = handler.MustNewPlaylistHandler
	MustNewStreamHandler       = handler.MustNewStreamHandler
	MustNewTrackHandler        = handler.MustNewTrackHandler
	NewAudioURLHandler         = handler.NewAudioURLHandler
	NewFeaturedArtistHandler   = handler.NewFeaturedArtistHandler
	NewLibraryHandler          = handler.NewLibraryHandler
	NewPlaylistHandler         = handler.NewPlaylistHandler
	NewStreamHandler           = handler.NewStreamHandler
	NewTrackHandler            = handler.NewTrackHandler
	WithPrefetchEnabled        = handler.WithPrefetchEnabled
	WithStreamWriteIdleTimeout = handler.WithStreamWriteIdleTimeout
)
