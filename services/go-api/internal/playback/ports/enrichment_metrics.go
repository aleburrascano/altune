package ports

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
