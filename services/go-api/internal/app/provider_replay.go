package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/httptrace"
)

// providerTransport is the single guard for fixture replay. Unless the explicit
// opt-in, a fixture dir and a non-prod ENV all hold, it returns nil so
// newClientFactory falls back to the shared live transport and no replayer is
// ever built. When enabled, every provider request is served from fixtures and
// an unmatched request errors rather than reaching the network.
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
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var fx struct {
		Exchanges []httptrace.Exchange `json:"exchanges"`
	}
	if err := json.Unmarshal(data, &fx); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return fx.Exchanges, nil
}
