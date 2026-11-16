package playback

import ports "altune/go-api/internal/playback/internal/core/ports"

type (
	EnrichmentMetrics    = ports.EnrichmentMetrics
	QueueStateRepository = ports.QueueStateRepository
	RateLimitMetrics     = ports.RateLimitMetrics
)

var ErrIdentityStoreUnavailable = ports.ErrIdentityStoreUnavailable
