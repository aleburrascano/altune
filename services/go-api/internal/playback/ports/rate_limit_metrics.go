package ports

type RateLimitMetrics interface {
	QueueStateRateLimited()
}

func NoopRateLimitMetrics() RateLimitMetrics { return noopRateLimitMetrics{} }

type noopRateLimitMetrics struct{}

func (noopRateLimitMetrics) QueueStateRateLimited() {}
