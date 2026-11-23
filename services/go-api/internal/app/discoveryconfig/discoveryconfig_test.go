package discoveryconfig_test

import (
	"altune/go-api/internal/app/discoveryconfig"
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/shared/config"
	"errors"
	"expvar"
	"net/http"
	"reflect"
	"testing"
	"time"
)

func TestDeps_CopiesEachConfigValueIntoItsOwnField(t *testing.T) {
	deps := discoveryconfig.Deps(t.Context(), distinctDiscoveryConfig(), http.DefaultTransport)

	wantProviders := discovery.ProviderSettings{
		MusicBrainzUserAgent: "mb-agent",
		DiscogsToken:         "discogs-token",
		FanartTVAPIKey:       "fanart-key",
		GeniusAccessToken:    "genius-token",
		TheAudioDBAPIKey:     "theaudiodb-key",
		GoogleSheetsAPIKey:   "sheets-key",
		TrackerHubSheetID:    "hub-sheet",
	}
	if !reflect.DeepEqual(deps.Providers, wantProviders) {
		t.Errorf("providers =\n  %+v\nwant\n  %+v", deps.Providers, wantProviders)
	}
	wantSearch := discovery.SearchSettings{
		Deadline:         1 * time.Second,
		ExplorationRate:  0.25,
		ProviderCacheTTL: 2 * time.Minute,
	}
	if deps.Search != wantSearch {
		t.Errorf("search =\n  %+v\nwant\n  %+v", deps.Search, wantSearch)
	}
	wantUnreleased := discovery.UnreleasedSettings{Tuning: discovery.UnreleasedConfig{
		MaxPages: 4, MaxDetails: 5, DetailConcurrency: 6,
		TTL: 7 * time.Hour, EmptyTTL: 8 * time.Hour, Wait: 9 * time.Second, JobTimeout: 10 * time.Second,
	}}
	if deps.Unreleased != wantUnreleased {
		t.Errorf("unreleased =\n  %+v\nwant\n  %+v", deps.Unreleased, wantUnreleased)
	}
}

func TestDeps_EachConfigSwitchTurnsOnOnlyItsOwnSetting(t *testing.T) {
	tests := []struct {
		name           string
		turnOn         func(*config.Config)
		wantProviders  discovery.ProviderSettings
		wantSearch     discovery.SearchSettings
		wantUnreleased discovery.UnreleasedSettings
	}{
		{"TailDemotionEnabled", func(c *config.Config) { c.TailDemotionEnabled = true }, discovery.ProviderSettings{}, discovery.SearchSettings{TailDemotion: true}, discovery.UnreleasedSettings{}},
		{"CrossKindProminenceEnabled", func(c *config.Config) { c.CrossKindProminenceEnabled = true }, discovery.ProviderSettings{}, discovery.SearchSettings{CrossKindProminence: true}, discovery.UnreleasedSettings{}},
		{"ExplorationEnabled", func(c *config.Config) { c.ExplorationEnabled = true }, discovery.ProviderSettings{}, discovery.SearchSettings{Exploration: true}, discovery.UnreleasedSettings{}},
		{"BehavioralRankingEnabled", func(c *config.Config) { c.BehavioralRankingEnabled = true }, discovery.ProviderSettings{}, discovery.SearchSettings{BehavioralRanking: true}, discovery.UnreleasedSettings{}},
		{"UnreleasedSource=cache", func(c *config.Config) { c.UnreleasedSource = "cache" }, discovery.ProviderSettings{}, discovery.SearchSettings{}, discovery.UnreleasedSettings{ServeFromCache: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			tt.turnOn(cfg)
			deps := discoveryconfig.Deps(t.Context(), cfg, http.DefaultTransport)
			if !reflect.DeepEqual(deps.Providers, tt.wantProviders) || deps.Search != tt.wantSearch || deps.Unreleased != tt.wantUnreleased {
				t.Errorf("deps =\n  %+v\n  %+v\n  %+v\nwant\n  %+v\n  %+v\n  %+v",
					deps.Providers, deps.Search, deps.Unreleased, tt.wantProviders, tt.wantSearch, tt.wantUnreleased)
			}
		})
	}
}

func distinctDiscoveryConfig() *config.Config {
	return &config.Config{
		MusicBrainzUserAgent:              "mb-agent",
		DiscogsToken:                      "discogs-token",
		FanartTVAPIKey:                    "fanart-key",
		GeniusAccessToken:                 "genius-token",
		TheAudioDBAPIKey:                  "theaudiodb-key",
		GoogleSheetsAPIKey:                "sheets-key",
		TrackerHubSheetID:                 "hub-sheet",
		DiscoverySearchDeadline:           1 * time.Second,
		ExplorationRate:                   0.25,
		DiscoveryProviderCacheTTL:         2 * time.Minute,
		GeniusUnreleasedMaxPages:          4,
		GeniusUnreleasedMaxDetails:        5,
		GeniusUnreleasedDetailConcurrency: 6,
		GeniusUnreleasedTTL:               7 * time.Hour,
		GeniusUnreleasedEmptyTTL:          8 * time.Hour,
		GeniusUnreleasedWait:              9 * time.Second,
		GeniusUnreleasedJobTimeout:        10 * time.Second,
	}
}

type stubTransport struct{}

func (stubTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("stub transport")
}

func TestDeps_CarriesTheLifecycleTransportTimeoutsAndCacheCounters(t *testing.T) {
	ctx := t.Context()
	transport := stubTransport{}

	deps := discoveryconfig.Deps(ctx, &config.Config{}, transport)

	if deps.Lifecycle != ctx || deps.Transport != transport {
		t.Error("deps do not carry the given lifecycle and transport")
	}
	if deps.ProviderTimeout != 10*time.Second || deps.ChartTimeout != 15*time.Second {
		t.Errorf("timeouts = %v, %v, want 10s and 15s", deps.ProviderTimeout, deps.ChartTimeout)
	}
	if deps.CacheOps == nil || deps.CacheOps != expvar.Get("discovery_cache_ops") {
		t.Error("deps do not count cache operations into the published discovery_cache_ops map")
	}
	if again := discoveryconfig.Deps(ctx, &config.Config{}, transport); again.CacheOps != deps.CacheOps {
		t.Error("a second build published a second cache counter map instead of reusing the first")
	}
}

func TestClientEvents_FollowTheTelemetryExportSetting(t *testing.T) {
	if discoveryconfig.ClientEvents(&config.Config{}) != nil {
		t.Error("no OTLP endpoint, yet discovery got client events to export")
	}
	if discoveryconfig.ClientEvents(&config.Config{OTLPEndpoint: "https://otlp.example.com"}) == nil {
		t.Error("an OTLP endpoint, yet discovery got no client events")
	}
}
