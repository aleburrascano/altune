package app

import (
	"net/http"
	"sync"
	"time"

	providermetrics "altune/go-api/internal/discovery/adapters/providermetrics"
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

// countedProviderTransport wraps a base provider transport in the per-provider,
// per-outcome counter whose counts back the operator-only
// /admin/metrics/live `providers` field. It belongs at the base of a chain, not
// above it: a correlated or recording transport layered on top then counts a
// round trip once rather than once per layer.
func countedProviderTransport(base http.RoundTripper) http.RoundTripper {
	return providermetrics.NewCountingTransport(base)
}

// sharedLiveTransport is the one live transport a caller that supplies none
// falls back to, so the process keeps a single rate limiter and connection pool
// per upstream host however many client factories exist. It is counted here,
// the single wrap point, so every adapter reached from the composition root —
// content, consensus, enrichment, artwork, the background chart clients and the
// admin replays alike — moves the provider counters.
var sharedLiveTransport = sync.OnceValue(func() http.RoundTripper {
	return countedProviderTransport(NewLiveTransport())
})

// clientFactory builds the HTTP clients the provider adapters take. Its
// transport is always concrete, so a factory handed to wiring code redirects
// every adapter that wiring builds.
type clientFactory struct {
	transport http.RoundTripper
}

// newClientFactory is the only place a nil transport resolves to the shared
// live one; construct every factory through it. A non-nil transport is taken
// verbatim: it is the caller's own chain, counted at the base it was built
// over, so the factory never adds a second counter to it.
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
