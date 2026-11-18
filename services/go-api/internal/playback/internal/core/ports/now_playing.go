package ports

import (
	shared "altune/go-api/internal/shared/kernel"
	"context"
	"errors"
)

var ErrTrackSourceUnavailable = errors.New("now-playing track source unavailable")

type TrackSource interface {
	NowPlayingTrack(ctx context.Context, userId shared.UserId, id shared.TrackId) (*NowPlayingTrack, error)
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
	Lookup(ctx context.Context, userId shared.UserId, trackId string) (*NowPlayingTrack, error)
}

type EnrichmentMetrics interface {
	EnrichmentFailed()
	NowPlayingLookupTimedOut()
	EnrichmentBreakerRejected()
	EnrichmentBreakerOpened()
	EnrichmentBreakerClosed()
}

func NoopEnrichmentMetrics() EnrichmentMetrics { return noopEnrichmentMetrics{} }

type noopEnrichmentMetrics struct{}

func (noopEnrichmentMetrics) EnrichmentFailed()          {}
func (noopEnrichmentMetrics) NowPlayingLookupTimedOut()  {}
func (noopEnrichmentMetrics) EnrichmentBreakerRejected() {}
func (noopEnrichmentMetrics) EnrichmentBreakerOpened()   {}
func (noopEnrichmentMetrics) EnrichmentBreakerClosed()   {}
