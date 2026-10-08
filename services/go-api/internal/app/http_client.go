package app

import (
	"net/http"
	"sync"
	"time"

	providermetrics "altune/go-api/internal/discovery/adapters/providermetrics"
	"altune/go-api/internal/shared/providerhttp"
)

const (
	discoveryHTTPTimeout = 10 * time.Second
	chartHTTPTimeout     = 15 * time.Second
)

func countedProviderTransport(base http.RoundTripper) http.RoundTripper {
	return providermetrics.NewCountingTransport(base)
}

var sharedLiveTransport = sync.OnceValue(func() http.RoundTripper {
	return countedProviderTransport(providerhttp.NewLiveTransport())
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

func (f clientFactory) chart() *http.Client {
	return &http.Client{Timeout: chartHTTPTimeout, Transport: f.transport}
}

func (f clientFactory) roundTripper() http.RoundTripper {
	return f.transport
}
