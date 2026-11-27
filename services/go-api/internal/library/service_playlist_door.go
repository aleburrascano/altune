package library

import playlist "altune/go-api/internal/library/internal/core/playlist"

type (
	PlaylistLifecycleService  = playlist.PlaylistLifecycleService
	PlaylistMembershipService = playlist.PlaylistMembershipService
)

var (
	ErrPlaylistClockRequired         = playlist.ErrClockRequired
	ErrPlaylistIDSourceRequired      = playlist.ErrIDSourceRequired
	ErrPlaylistCollaboratorRequired  = playlist.ErrCollaboratorRequired
	MustNewPlaylistLifecycleService  = playlist.MustNewPlaylistLifecycleService
	MustNewPlaylistMembershipService = playlist.MustNewPlaylistMembershipService
	NewPlaylistLifecycleService      = playlist.NewPlaylistLifecycleService
	NewPlaylistMembershipService     = playlist.NewPlaylistMembershipService
)

var ErrTooManyPlaylists = playlist.ErrTooManyPlaylists
