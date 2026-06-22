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

// newDiscoveryClient is the standard discovery provider HTTP client.
func newDiscoveryClient() *http.Client {
	return &http.Client{Timeout: discoveryHTTPTimeout}
}

// newChartClient is the longer-timeout client for chart/vocabulary fetches.
func newChartClient() *http.Client {
	return &http.Client{Timeout: chartHTTPTimeout}
}
