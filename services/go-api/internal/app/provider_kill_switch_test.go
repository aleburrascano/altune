package app

import (
	"altune/go-api/internal/shared/config"
	"testing"

	discoveryDomain "altune/go-api/internal/discovery/domain"
)

// allScrapedProvidersEnabled mirrors the production defaults: every
// reverse-engineered provider is wired unless an operator opts out.
func allScrapedProvidersEnabled() config.Config {
	return config.Config{
		SpotifyEnabled:     true,
		SoundCloudEnabled:  true,
		AppleMusicEnabled:  true,
		AmazonMusicEnabled: true,
		YtMusicEnabled:     true,
	}
}

// TestScrapedProviderKillSwitch reproduces the gap where the Spotify,
// SoundCloud, Apple Music, Amazon Music and YTMusic adapters were wired
// unconditionally: no config value could pull one out, so a scraped endpoint
// that broke or had to be withdrawn needed a code change and redeploy. Each
// must now be dropped from every discovery wiring site when its flag is off.
func TestScrapedProviderKillSwitch(t *testing.T) {
	tests := []struct {
		name          string
		disable       func(*config.Config)
		provider      discoveryDomain.ProviderName
		consensusName string
		inArtistMap   bool
	}{
		{"spotify", func(c *config.Config) { c.SpotifyEnabled = false }, discoveryDomain.ProviderSpotify, "", true},
		{"soundcloud", func(c *config.Config) { c.SoundCloudEnabled = false }, discoveryDomain.ProviderSoundCloud, "soundcloud", true},
		{"applemusic", func(c *config.Config) { c.AppleMusicEnabled = false }, discoveryDomain.ProviderAppleMusic, "", true},
		{"amazonmusic", func(c *config.Config) { c.AmazonMusicEnabled = false }, discoveryDomain.ProviderAmazonMusic, "", false},
		{"ytmusic", func(c *config.Config) { c.YtMusicEnabled = false }, discoveryDomain.ProviderYouTube, "ytmusic", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enabled := allScrapedProvidersEnabled()
			if !hasSearchProvider(&enabled, tt.provider) {
				t.Fatalf("precondition: %s must be wired when enabled", tt.provider)
			}

			cfg := allScrapedProvidersEnabled()
			tt.disable(&cfg)

			if hasSearchProvider(&cfg, tt.provider) {
				t.Errorf("search providers still include %s after disabling it", tt.provider)
			}
			if tt.inArtistMap {
				if _, ok := buildArtistContentProviders(newClientFactory(nil), &enabled)[tt.provider]; !ok {
					t.Fatalf("precondition: %s must be an artist content provider when enabled", tt.provider)
				}
				if _, ok := buildArtistContentProviders(newClientFactory(nil), &cfg)[tt.provider]; ok {
					t.Errorf("artist content providers still include %s after disabling it", tt.provider)
				}
			}
			if tt.consensusName != "" {
				if !hasConsensusProvider(&enabled, tt.consensusName) {
					t.Fatalf("precondition: consensus must include %s when enabled", tt.consensusName)
				}
				if hasConsensusProvider(&cfg, tt.consensusName) {
					t.Errorf("consensus providers still include %s after disabling it", tt.consensusName)
				}
			}
		})
	}
}

func hasSearchProvider(cfg *config.Config, want discoveryDomain.ProviderName) bool {
	for _, p := range buildSearchProviderList(newClientFactory(nil), cfg, nil) {
		if p.Name() == want {
			return true
		}
	}
	return false
}

func hasConsensusProvider(cfg *config.Config, want string) bool {
	for _, p := range BuildConsensusProviders(cfg, nil) {
		if p.Name == want {
			return true
		}
	}
	return false
}
