package playback

import (
	nowplaying "altune/go-api/internal/playback/internal/adapters/nowplaying"
	ports "altune/go-api/internal/playback/internal/core/ports"
)

type TrackSource = ports.TrackSource

var (
	ErrNowPlayingClockRequired        = nowplaying.ErrClockRequired
	ErrNowPlayingCollaboratorRequired = nowplaying.ErrCollaboratorRequired
	ErrNowPlayingInvalidTimeout       = nowplaying.ErrInvalidTimeout
	ErrTrackSourceUnavailable         = ports.ErrTrackSourceUnavailable
	ErrNowPlayingTrackNotFound        = ports.ErrNowPlayingTrackNotFound
)

var (
	MustNewNowPlayingReader = nowplaying.MustNewNowPlayingReader
	NewNowPlayingReader     = nowplaying.NewNowPlayingReader
)
