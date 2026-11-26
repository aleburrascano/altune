package searchsettings_test

import (
	"altune/go-api/internal/app/searchsettings"
	"altune/go-api/internal/search"
	"altune/go-api/internal/shared/config"
	"errors"
	"expvar"
	"net/http"
	"reflect"
	"testing"
	"time"
)

func TestDeps_CopiesEachConfigValueIntoItsOwnField(t *testing.T) {
	deps := searchsettings.Deps(t.Context(), distinctDiscoveryConfig(), http.DefaultTransport)

	wantProviders := search.ProviderSettings{
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
	wantSearch := search.SearchSettings{
		Deadline:         1 * time.Second,
		ExplorationRate:  0.25,
		ProviderCacheTTL: 2 * time.Minute,
	}
	if deps.Search != wantSearch {
		t.Errorf("search =\n  %+v\nwant\n  %+v", deps.Search, wantSearch)
	}
	wantUnreleased := search.UnreleasedSettings{Tuning: search.UnreleasedConfig{
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
		wantProviders  search.ProviderSettings
		wantSearch     search.SearchSettings
		wantUnreleased search.UnreleasedSettings
	}{
		{"TailDemotionEnabled", func(c *config.Config) { c.TailDemotionEnabled = true }, search.ProviderSettings{}, search.SearchSettings{TailDemotion: true}, search.UnreleasedSettings{}},
		{"CrossKindProminenceEnabled", func(c *config.Config) { c.CrossKindProminenceEnabled = true }, search.ProviderSettings{}, search.SearchSettings{CrossKindProminence: true}, search.UnreleasedSettings{}},
		{"ExplorationEnabled", func(c *config.Config) { c.ExplorationEnabled = true }, search.ProviderSettings{}, search.SearchSettings{Exploration: true}, search.UnreleasedSettings{}},
		{"BehavioralRankingEnabled", func(c *config.Config) { c.BehavioralRankingEnabled = true }, search.ProviderSettings{}, search.SearchSettings{BehavioralRanking: true}, search.UnreleasedSettings{}},
		{"UnreleasedSource=cache", func(c *config.Config) { c.UnreleasedSource = "cache" }, search.ProviderSettings{}, search.SearchSettings{}, search.UnreleasedSettings{ServeFromCache: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			tt.turnOn(cfg)
			deps := searchsettings.Deps(t.Context(), cfg, http.DefaultTransport)
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

	deps := searchsettings.Deps(ctx, &config.Config{}, transport)

	if deps.Lifecycle != ctx || deps.Transport != transport {
		t.Error("deps do not carry the given lifecycle and transport")
	}
	if deps.ProviderTimeout != 10*time.Second || deps.ChartTimeout != 15*time.Second {
		t.Errorf("timeouts = %v, %v, want 10s and 15s", deps.ProviderTimeout, deps.ChartTimeout)
	}
	if deps.CacheOps == nil || deps.CacheOps != expvar.Get("discovery_cache_ops") {
		t.Error("deps do not count cache operations into the published discovery_cache_ops map")
	}
	if again := searchsettings.Deps(ctx, &config.Config{}, transport); again.CacheOps != deps.CacheOps {
		t.Error("a second build published a second cache counter map instead of reusing the first")
	}
}

func TestClientEvents_FollowTheTelemetryExportSetting(t *testing.T) {
	if _, off := searchsettings.ClientEvents(&config.Config{}).(search.NoClientEvents); !off {
		t.Error("no OTLP endpoint, yet discovery got client events to export")
	}
	if _, off := searchsettings.ClientEvents(&config.Config{OTLPEndpoint: "https://otlp.example.com"}).(search.NoClientEvents); off {
		t.Error("an OTLP endpoint, yet discovery got no client events")
	}
}
