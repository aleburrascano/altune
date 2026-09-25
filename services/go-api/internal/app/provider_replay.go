package app

import (
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/httptrace"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
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
