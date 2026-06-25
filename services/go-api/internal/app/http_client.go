package app

import (
	"net/http"
	"time"
)

// HTTP-client policy for the discovery providers, in one place. Previously every
// provider construction inlined its own `&http.Client{Timeout: ...}` (≈30 sites,
// with a couple silently diverging to 15s), so the timeout policy was invisible
// and easy to copy wrong. These factories are the single source of that policy;
// each returns a fresh client (own connection pool, unchanged from before).
const (
	discoveryHTTPTimeout = 10 * time.Second // default for discovery provider calls
	chartHTTPTimeout     = 15 * time.Second // chart fetches: larger payloads, looser bound
)

// defaultLiveTransport paces and retries production provider calls. It is shared
// process-wide so the per-host rate limits hold across every provider client (a
// per-client limiter would let N providers each do N× the host's limit).
var defaultLiveTransport = NewLiveTransport()

// clientFactory builds discovery provider HTTP clients, optionally over a shared
// transport. The zero value uses the rate-limiting, retrying live transport (the
// production path). Offline tooling (the deterministic eval) injects a
// record/replay transport so the same wiring runs against frozen provider
// responses — no drift from a hand-reconstructed pipeline.
type clientFactory struct {
	transport http.RoundTripper // nil → defaultLiveTransport
}

// transport for clients: the injected one (record/replay) when set, else the
// shared rate-limiting live transport.
func (f clientFactory) clientTransport() http.RoundTripper {
	if f.transport != nil {
		return f.transport
	}
	return defaultLiveTransport
}

// discovery is the standard discovery provider HTTP client.
func (f clientFactory) discovery() *http.Client {
	return &http.Client{Timeout: discoveryHTTPTimeout, Transport: f.clientTransport()}
}

// chart is the longer-timeout client for chart/vocabulary fetches.
func (f clientFactory) chart() *http.Client {
	return &http.Client{Timeout: chartHTTPTimeout, Transport: f.clientTransport()}
}

// roundTripper exposes the transport for providers that own their HTTP client and
// only accept a transport (YouTube Music, whose ytmusic library uses a
// package-global client).
func (f clientFactory) roundTripper() http.RoundTripper {
	return f.clientTransport()
}

// newDiscoveryClient is the standard discovery provider HTTP client (default
// transport). Retained for the non-search call sites (chart refresh, consensus,
// single-artist diagnostics) that have no transport to inject.
func newDiscoveryClient() *http.Client {
	return clientFactory{}.discovery()
}

// newChartClient is the longer-timeout client for chart/vocabulary fetches.
func newChartClient() *http.Client {
	return clientFactory{}.chart()
}
