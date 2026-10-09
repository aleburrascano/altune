package ports

import (
	"altune/go-api/internal/shared"
	"context"
)

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
