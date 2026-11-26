package ports

import (
	"altune/go-api/internal/shared/ids"
	"context"
	"errors"
)

var ErrTrackSourceUnavailable = errors.New("now-playing track source unavailable")

var ErrNowPlayingTrackNotFound = errors.New("now-playing track not found")

type TrackSource interface {
	NowPlayingTrack(ctx context.Context, userId ids.UserId, id ids.TrackId) (*NowPlayingTrack, error)
}

type NowPlayingTrack struct {
	Id                string
	Title             string
	Artist            string
	ArtworkURL        *string
	DurationSeconds   *float64
	AcquisitionStatus string
}

type NowPlayingReader interface {
	Lookup(ctx context.Context, userId ids.UserId, trackId string) (*NowPlayingTrack, error)
}

type EnrichmentMetrics interface {
	EnrichmentFailed()
	NowPlayingLookupTimedOut()
	EnrichmentBreakerRejected()
	EnrichmentBreakerOpened()
	EnrichmentBreakerClosed()
}
