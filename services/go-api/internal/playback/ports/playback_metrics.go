package ports

type EnrichmentMetrics interface {
	EnrichmentFailed()
	NowPlayingLookupTimedOut()
	EnrichmentBreakerRejected()
	EnrichmentBreakerOpened()
	EnrichmentBreakerClosed()
}

type QueueStateMetrics interface {
	CorruptStoredState()
	QueueStateOpTimedOut()
}

type RateLimitMetrics interface {
	QueueStateRateLimited()
}

type ErasureSweepMetrics interface {
	SweepIdle()
	QueueStateErased(n int)
}

func NoopEnrichmentMetrics() EnrichmentMetrics { return noopEnrichmentMetrics{} }

func NoopQueueStateMetrics() QueueStateMetrics { return noopQueueStateMetrics{} }

func NoopRateLimitMetrics() RateLimitMetrics { return noopRateLimitMetrics{} }

func NoopErasureSweepMetrics() ErasureSweepMetrics { return noopErasureSweepMetrics{} }

type noopErasureSweepMetrics struct{}

func (noopErasureSweepMetrics) SweepIdle()           {}
func (noopErasureSweepMetrics) QueueStateErased(int) {}

type noopEnrichmentMetrics struct{}

func (noopEnrichmentMetrics) EnrichmentFailed()          {}
func (noopEnrichmentMetrics) NowPlayingLookupTimedOut()  {}
func (noopEnrichmentMetrics) EnrichmentBreakerRejected() {}
func (noopEnrichmentMetrics) EnrichmentBreakerOpened()   {}
func (noopEnrichmentMetrics) EnrichmentBreakerClosed()   {}

type noopQueueStateMetrics struct{}

func (noopQueueStateMetrics) CorruptStoredState()   {}
func (noopQueueStateMetrics) QueueStateOpTimedOut() {}

type noopRateLimitMetrics struct{}

func (noopRateLimitMetrics) QueueStateRateLimited() {}
