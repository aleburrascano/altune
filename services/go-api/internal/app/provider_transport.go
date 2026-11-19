package app

import (
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/shared/egress"
	"net/http"
	"sync"
	"time"
)

const (
	discoveryHTTPTimeout = 10 * time.Second
	chartHTTPTimeout     = 15 * time.Second
)

func countedProviderTransport(base http.RoundTripper) http.RoundTripper {
	return discovery.NewCountingTransport(base)
}

var sharedLiveTransport = sync.OnceValue(func() http.RoundTripper {
	return countedProviderTransport(egress.NewLiveTransport(egress.WithRetryRecorder(discovery.RecordRetry)))
})

type clientFactory struct {
	transport http.RoundTripper
}

func newClientFactory(transport http.RoundTripper) clientFactory {
	if transport == nil {
		return clientFactory{transport: sharedLiveTransport()}
	}
	return clientFactory{transport: transport}
}

func (f clientFactory) discovery() *http.Client {
	return &http.Client{Timeout: discoveryHTTPTimeout, Transport: f.transport}
}

func (f clientFactory) roundTripper() http.RoundTripper {
	return f.transport
}
