package providerhttp

import (
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/shared/egress"
	"net/http"
	"sync"
)

var Live = sync.OnceValue(func() http.RoundTripper {
	return discovery.NewCountingTransport(egress.NewLiveTransport(egress.WithRetryRecorder(discovery.RecordRetry)))
})
