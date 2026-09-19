package app

import (
	"net/http"
	"sync"
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

// sharedLiveTransport is the one live transport a caller that supplies none
// falls back to, so the process keeps a single rate limiter and connection pool
// per upstream host however many client factories exist.
var sharedLiveTransport = sync.OnceValue(NewLiveTransport)

// clientFactory builds the HTTP clients the provider adapters take. Its
// transport is always concrete, so a factory handed to wiring code redirects
// every adapter that wiring builds.
type clientFactory struct {
	transport http.RoundTripper
}

// newClientFactory is the only place a nil transport resolves to the shared
// live one; construct every factory through it.
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
