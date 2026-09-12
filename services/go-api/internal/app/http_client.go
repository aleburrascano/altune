package app

import (
	"net/http"
	"time"
)

const (
	discoveryHTTPTimeout = 10 * time.Second
	chartHTTPTimeout     = 15 * time.Second
)

// liveMaxConnsPerHost bounds concurrent connections to any single upstream
// host so provider traffic cannot exhaust local sockets or hammer a provider
// without limit; ops can tune it.
const liveMaxConnsPerHost = 8

// baseTransport clones the default transport and caps per-host concurrency.
func baseTransport() http.RoundTripper {
	t, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return http.DefaultTransport
	}
	c := t.Clone()
	c.MaxConnsPerHost = liveMaxConnsPerHost
	return c
}

var defaultLiveTransport = NewLiveTransport()

type clientFactory struct {
	transport http.RoundTripper
}

func (f clientFactory) clientTransport() http.RoundTripper {
	if f.transport != nil {
		return f.transport
	}
	return defaultLiveTransport
}

func (f clientFactory) discovery() *http.Client {
	return &http.Client{Timeout: discoveryHTTPTimeout, Transport: f.clientTransport()}
}

func (f clientFactory) chart() *http.Client {
	return &http.Client{Timeout: chartHTTPTimeout, Transport: f.clientTransport()}
}

func (f clientFactory) roundTripper() http.RoundTripper {
	return f.clientTransport()
}

func newDiscoveryClient() *http.Client {
	return clientFactory{}.discovery()
}

func newChartClient() *http.Client {
	return clientFactory{}.chart()
}
