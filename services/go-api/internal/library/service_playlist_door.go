package library

import playlist "altune/go-api/internal/library/internal/core/playlist"

type (
	PlaylistLifecycleService  = playlist.PlaylistLifecycleService
	PlaylistMembershipService = playlist.PlaylistMembershipService
)

var (
	NewPlaylistLifecycleService  = playlist.NewPlaylistLifecycleService
	NewPlaylistMembershipService = playlist.NewPlaylistMembershipService
	WithPlaylistLifecycleEvents  = playlist.WithPlaylistLifecycleEvents
	WithPlaylistMembershipEvents = playlist.WithPlaylistMembershipEvents
)

var ErrTooManyPlaylists = playlist.ErrTooManyPlaylists
