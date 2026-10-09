package app

import (
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/httptrace"
	"altune/go-api/internal/shared/providerhttp"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	providermetrics "altune/go-api/internal/discovery/adapters/providermetrics"
)

func providerTransport(cfg *config.Config) (http.RoundTripper, error) {
	if !cfg.ProviderReplayEnabled() {
		return nil, nil
	}
	exchanges, err := loadReplayFixtures(cfg.ProviderReplayDir)
	if err != nil {
		return nil, err
	}
	return httptrace.NewStickyReplayer(exchanges), nil
}

func loadReplayFixtures(dir string) ([]httptrace.Exchange, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("no fixtures in %s", dir)
	}
	var all []httptrace.Exchange
	for _, p := range paths {
		exchanges, err := readReplayFixture(p)
		if err != nil {
			return nil, err
		}
		all = append(all, exchanges...)
	}
	return all, nil
}

func readReplayFixture(path string) ([]httptrace.Exchange, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var fx struct {
		Exchanges []httptrace.Exchange `json:"exchanges"`
	}
	if err := json.Unmarshal(raw, &fx); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return fx.Exchanges, nil
}

const (
	discoveryHTTPTimeout = 10 * time.Second
	chartHTTPTimeout     = 15 * time.Second
)

func countedProviderTransport(base http.RoundTripper) http.RoundTripper {
	return providermetrics.NewCountingTransport(base)
}

var sharedLiveTransport = sync.OnceValue(func() http.RoundTripper {
	return countedProviderTransport(providerhttp.NewLiveTransport())
})

type clientFactory struct {
	transport http.RoundTripper
}

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
