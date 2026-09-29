package app

import (
	"altune/go-api/internal/shared/config"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"syscall"
	"testing"

	providermetrics "altune/go-api/internal/discovery/adapters/providermetrics"
)

const stubbedChartTerm = "stubbed chart term"

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

func TestTheDefaultTransportCountsEveryProviderCall(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	before := providermetrics.ReadSnapshot()
	counting := countedProviderTransport(&http.Transport{DialContext: allowLoopbackThenGuard})
	resp, err := newClientFactory(counting).discovery().Get(upstream.URL)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	_ = resp.Body.Close()

	counted := totalProviderCounts(providermetrics.ReadSnapshot()) - totalProviderCounts(before)
	if counted != 1 {
		t.Errorf("provider counters moved by %d over one call on the default transport, want 1", counted)
	}
}

func TestDiscoveryClientRefusesALoopbackUpstream(t *testing.T) {
	connections := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		connections++
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	resp, err := newClientFactory(nil).discovery().Get(upstream.URL)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("a loopback upstream was fetched through the discovery client")
	}
	if !errors.Is(err, errNonPublicProviderAddr) {
		t.Errorf("error = %v, want the non-public address refusal", err)
	}
	if connections != 0 {
		t.Errorf("upstream saw %d requests, want 0", connections)
	}
}

func allowLoopbackThenGuard(ctx context.Context, network, address string) (net.Conn, error) {
	guarded := &net.Dialer{Control: func(network, address string, c syscall.RawConn) error {
		if strings.HasPrefix(address, "127.0.0.1:") {
			return nil
		}
		return refuseNonPublicDial(network, address, c)
	}}
	return guarded.DialContext(ctx, network, address)
}

func TestDiscoveryClientRefusesARedirectToTheMetadataAddress(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
	}))
	defer upstream.Close()
	client := &http.Client{Transport: &http.Transport{DialContext: allowLoopbackThenGuard}}

	resp, err := client.Get(upstream.URL)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("the redirect to the metadata address was followed")
	}
	if !errors.Is(err, errNonPublicProviderAddr) {
		t.Errorf("error = %v, want the non-public address refusal", err)
	}
}

func TestBaseTransportKeepsItsConnectionCapAndRefusesNonPublicAddresses(t *testing.T) {
	tr, ok := baseTransport().(*http.Transport)
	if !ok {
		t.Fatal("baseTransport is not an *http.Transport")
	}
	if tr.MaxConnsPerHost != liveMaxConnsPerHost {
		t.Errorf("MaxConnsPerHost = %d, want %d", tr.MaxConnsPerHost, liveMaxConnsPerHost)
	}
	for _, addr := range []string{"10.0.0.1:80", "169.254.169.254:80", "[::1]:80", "100.64.0.1:80"} {
		if err := refuseNonPublicDial("tcp", addr, nil); !errors.Is(err, errNonPublicProviderAddr) {
			t.Errorf("%s: err = %v, want refusal", addr, err)
		}
	}
	if err := refuseNonPublicDial("tcp", "93.184.216.34:443", nil); err != nil {
		t.Errorf("public address refused: %v", err)
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
