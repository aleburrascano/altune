package app

import (
	"altune/go-api/internal/shared/config"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

const stubbedChartTerm = "stubbed chart term"

// cannedChartsRT answers every request with a single chart item, so a provider
// built over it yields a term no live upstream would.
type cannedChartsRT struct{}

func (cannedChartsRT) RoundTrip(_ *http.Request) (*http.Response, error) {
	body := `{"data":[{"title":"` + stubbedChartTerm + `","name":"` + stubbedChartTerm + `"}]}`
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}, nil
}

func TestNilTransportResolvesToOneSharedLiveTransport(t *testing.T) {
	first := newClientFactory(nil).roundTripper()
	second := newClientFactory(nil).roundTripper()

	if first == nil {
		t.Fatal("a factory over a nil transport must carry the live transport, not nil")
	}
	if first != second {
		t.Error("nil-transport factories hold different transports; per-host rate limiters are no longer shared")
	}
}

func TestChartProvidersFetchOverTheGivenFactorysTransport(t *testing.T) {
	a := &App{cfg: &config.Config{}}

	charts := a.buildChartProviders(newClientFactory(cannedChartsRT{}))

	if len(charts) != 1 {
		t.Fatalf("chart providers = %d, want 1 (Deezer; Last.fm unconfigured)", len(charts))
	}
	entries, err := charts[0].FetchCharts(context.Background(), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("chart provider returned nothing, so it never read the factory's transport")
	}
	for _, e := range entries {
		if e.Term != stubbedChartTerm {
			t.Errorf("chart term = %q, want the stub transport's %q", e.Term, stubbedChartTerm)
		}
	}
}
