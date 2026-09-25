package app

import (
	providermetrics "altune/go-api/internal/discovery/adapters/providermetrics"
	"altune/go-api/internal/shared/config"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// countingProviderRT answers every provider request with an empty JSON body and
// counts the round trips it served, so a test can compare what the wiring
// observed against what actually left the process.
type countingProviderRT struct {
	mu    sync.Mutex
	calls int
}

func (rt *countingProviderRT) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.mu.Lock()
	rt.calls++
	rt.mu.Unlock()
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader("{}")),
		Request:    req,
	}, nil
}

func (rt *countingProviderRT) roundTrips() int {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.calls
}

func totalProviderCounts(s providermetrics.Snapshot) int64 {
	var total int64
	for _, o := range s {
		total += o.OK + o.Quota + o.Error
	}
	return total
}

func TestRequestPathProviderCallsAreCountedOnce(t *testing.T) {
	routes := []struct {
		name   string
		target string
	}{
		{"album content", "/albums/deezer/1/tracks"},
		{"lyrics", "/lyrics?title=song&subtitle=artist"},
		{"enrichment", "/enrichment?kind=album&title=album"},
	}

	for _, route := range routes {
		t.Run(route.name, func(t *testing.T) {
			rt := &countingProviderRT{}
			a := &App{cfg: &config.Config{MusicBrainzUserAgent: "altune-test/1.0"}}
			// The fake enters counted, where the composition root counts its own
			// live base, so a second counter anywhere above it in the wiring
			// surfaces here as a doubled count.
			disc := a.wireDiscovery(context.Background(), newClientFactory(countedProviderTransport(rt)))
			before := providermetrics.ReadSnapshot()
			disc.handler.Routes().ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, route.target, nil))
			counted := totalProviderCounts(providermetrics.ReadSnapshot()) - totalProviderCounts(before)

			if rt.roundTrips() == 0 {
				t.Fatalf("%s made no provider call, so it proves nothing about the transport", route.target)
			}
			if counted != int64(rt.roundTrips()) {
				t.Errorf("provider counters moved by %d over %d provider calls, want one count per call", counted, rt.roundTrips())
			}
		})
	}
}
