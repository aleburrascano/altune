package playback

import (
	nowplaying "altune/go-api/internal/playback/internal/adapters/nowplaying"
	ports "altune/go-api/internal/playback/internal/core/ports"
)

type TrackSource = ports.TrackSource

var (
	ErrTrackSourceUnavailable  = ports.ErrTrackSourceUnavailable
	ErrNowPlayingTrackNotFound = ports.ErrNowPlayingTrackNotFound
)

var NewNowPlayingReader = nowplaying.NewNowPlayingReader
