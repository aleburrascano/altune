package app

import (
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/httptrace"
	"altune/go-api/internal/shared/providerhttp"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	providermetrics "altune/go-api/internal/discovery/adapters/providermetrics"
)

func writeFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	body := `{"label":"x","exchanges":[{"method":"GET","url":"http://example.test/search?q=a","status":200,"response_body":"{\"ok\":true}"}]}`
	if err := os.WriteFile(filepath.Join(dir, "corpus.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestProviderTransport_ProdNeverBuildsReplayer(t *testing.T) {
	dir := writeFixture(t)
	cases := []config.Config{
		{Env: "production", ProviderReplayOptIn: true, ProviderReplayDir: dir},
		{Env: "", ProviderReplayOptIn: true, ProviderReplayDir: dir},
		{Env: "test", ProviderReplayOptIn: false, ProviderReplayDir: dir},
		{Env: "test", ProviderReplayOptIn: true},
	}
	for _, c := range cases {
		rt, err := providerTransport(&c)
		if err != nil || rt != nil {
			t.Fatalf("cfg %+v: got transport %v err %v, want nil", c, rt, err)
		}
	}
}

func TestProviderTransport_ReplaysStickyAndErrorsOnMiss(t *testing.T) {
	cfg := &config.Config{Env: "test", ProviderReplayOptIn: true, ProviderReplayDir: writeFixture(t)}
	rt, err := providerTransport(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := rt.(*httptrace.Replayer); !ok {
		t.Fatalf("got %T, want *httptrace.Replayer", rt)
	}
	for i := 0; i < 2; i++ {
		req, _ := http.NewRequest(http.MethodGet, "http://example.test/search?q=a", nil)
		resp, err := rt.RoundTrip(req)
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		_ = resp.Body.Close()
	}
	miss, _ := http.NewRequest(http.MethodGet, "http://example.test/other", nil)
	if resp, err := rt.RoundTrip(miss); err == nil {
		_ = resp.Body.Close()
		t.Fatal("unmatched request must error")
	}
}

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
	if !errors.Is(err, providerhttp.ErrNonPublicAddr) {
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
		return providerhttp.RefuseNonPublicDial(network, address, c)
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
	if !errors.Is(err, providerhttp.ErrNonPublicAddr) {
		t.Errorf("error = %v, want the non-public address refusal", err)
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
