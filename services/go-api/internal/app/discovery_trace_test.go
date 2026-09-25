package app

import (
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/logging"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	providermetrics "altune/go-api/internal/discovery/adapters/providermetrics"
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

// TestRequestPathProviderCallsAreTracedAndCountedOnce reproduces #2015: only the
// search service was built over the correlated, counting transport, so a
// content, lyrics or enrichment request left no Exchange in its trace and moved
// no provider counter — a Deezer content outage was invisible on
// /admin/metrics/live. Each route below is served by a different family of
// adapters (album content, lyrics, MusicBrainz enrichment and the artwork chain
// it fans out to), and all of them are built from the one factory wireDiscovery
// wraps, so a call that escapes the wrap fails here.
func TestRequestPathProviderCallsAreTracedAndCountedOnce(t *testing.T) {
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
			corrID := "corr-" + strings.ReplaceAll(route.name, " ", "-")

			before := providermetrics.ReadSnapshot()
			disc.handler.Routes().ServeHTTP(httptest.NewRecorder(), correlatedRequest(route.target, corrID))
			counted := totalProviderCounts(providermetrics.ReadSnapshot()) - totalProviderCounts(before)

			if rt.roundTrips() == 0 {
				t.Fatalf("%s made no provider call, so it proves nothing about the transport", route.target)
			}
			record, found := disc.requestStore.Get(corrID)
			if !found || len(record.Exchanges) == 0 {
				t.Errorf("%s recorded no exchange under its correlation id; the adapter bypassed the traced transport", route.target)
			}
			if counted != int64(rt.roundTrips()) {
				t.Errorf("provider counters moved by %d over %d provider calls, want one count per call", counted, rt.roundTrips())
			}
		})
	}
}

// TestBackgroundChartCallsAreCountedButStayOffTheTrace pins the other half of
// the wrap decision: chart refresh runs under no request, so tracing it would
// only cost the trace store memory for exchanges no correlation id can ever
// reach — but its calls leave the process like any other provider call, so
// /admin/metrics/live must see them.
func TestBackgroundChartCallsAreCountedButStayOffTheTrace(t *testing.T) {
	rt := &countingProviderRT{}
	base := newClientFactory(countedProviderTransport(rt))
	a := &App{cfg: &config.Config{}}

	disc := a.wireDiscovery(context.Background(), base)
	charts := a.buildChartProviders(base)
	if len(charts) == 0 {
		t.Fatal("precondition: the Deezer chart provider must be wired")
	}
	before := providermetrics.ReadSnapshot()
	if _, err := charts[0].FetchCharts(logging.WithCorrelationID(context.Background(), "corr-chart"), 1); err != nil {
		t.Fatalf("fetch charts: %v", err)
	}
	counted := totalProviderCounts(providermetrics.ReadSnapshot()) - totalProviderCounts(before)

	if rt.roundTrips() == 0 {
		t.Fatal("the chart fetch made no provider call, so it proves nothing about the transport")
	}
	if counted != int64(rt.roundTrips()) {
		t.Errorf("provider counters moved by %d over %d chart calls, want one count per call", counted, rt.roundTrips())
	}
	if _, found := disc.requestStore.Get("corr-chart"); found {
		t.Error("a chart fetch reached the request trace store; background traffic must stay off the correlated transport")
	}
}

func correlatedRequest(target, corrID string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	return req.WithContext(logging.WithCorrelationID(req.Context(), corrID))
}
