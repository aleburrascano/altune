package app

import (
	"altune/go-api/internal/shared/config"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"testing"
)

// scrapedProvidersConfig returns a config with the three scraped-credential
// kill switches (Apple Music, Spotify, SoundCloud) set to enabled and every
// key-gated provider left unconfigured, so the wiring collections vary only
// with the switches under test.
func scrapedProvidersConfig(enabled bool) *config.Config {
	return &config.Config{
		AppleMusicEnabled: enabled,
		SpotifyEnabled:    enabled,
		SoundCloudEnabled: enabled,
	}
}

// typeName renders a value's dynamic type, e.g. "*providers.SpotifyAdapter".
func typeName(v reflect.Value) string {
	if v.Kind() == reflect.Interface {
		v = v.Elem()
	}
	return v.Type().String()
}

func soundCloudFallbackIsNil(t *testing.T, v any) bool {
	t.Helper()
	return reflect.ValueOf(v).Elem().FieldByName("fallback").IsNil()
}

// TestScrapedProviderWiringCollections pins which adapters each wiring builder
// puts into its collection, in order, with the Apple Music / Spotify /
// SoundCloud kill switches on and off, so collapsing their construction into
// one builder each stays behavior-preserving.
func TestScrapedProviderWiringCollections(t *testing.T) {
	tests := []struct {
		name    string
		enabled bool
		artist  []string
		search  []string
		consens []string
		artwork []string
	}{
		{
			name:    "switches on",
			enabled: true,
			artist: []string{
				"applemusic=*providers.AppleMusicAdapter", "deezer=*providers.DeezerAdapter",
				"soundcloud=*providers.SoundCloudAPIAdapter", "spotify=*providers.SpotifyAdapter",
			},
			search: []string{
				"*providers.DeezerAdapter", "*providers.AppleMusicAdapter",
				"*providers.SoundCloudAPIAdapter", "*providers.YouTubeMusicAdapter", "*providers.SpotifyAdapter",
			},
			consens: []string{"itunes", "ytmusic", "soundcloud"},
			artwork: []string{
				"*providers.CoverArtArchiveResolver", "*providers.SpotifyArtworkResolver",
				"*providers.TheAudioDBAdapter", "*providers.DeezerAdapter", "*providers.ITunesAdapter",
				"*providers.YouTubeMusicArtworkResolver", "*providers.SoundCloudAPIAdapter",
			},
		},
		{
			name:    "switches off",
			enabled: false,
			artist:  []string{"deezer=*providers.DeezerAdapter"},
			search:  []string{"*providers.DeezerAdapter", "*providers.YouTubeMusicAdapter"},
			consens: []string{"itunes", "ytmusic"},
			artwork: []string{
				"*providers.CoverArtArchiveResolver", "*providers.TheAudioDBAdapter",
				"*providers.DeezerAdapter", "*providers.ITunesAdapter", "*providers.YouTubeMusicArtworkResolver",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := scrapedProvidersConfig(tt.enabled)
			cfg.YtMusicEnabled = true

			var artist []string
			for name, p := range buildArtistContentProviders(newClientFactory(nil), cfg) {
				artist = append(artist, fmt.Sprintf("%s=%s", name, typeName(reflect.ValueOf(p))))
			}
			sort.Strings(artist)
			assertEqual(t, "artist content providers", artist, tt.artist)

			var search []string
			for _, p := range BuildDiscoveryProviders(cfg, nil) {
				search = append(search, typeName(reflect.ValueOf(p)))
				if typeName(reflect.ValueOf(p)) == "*providers.SoundCloudAPIAdapter" && soundCloudFallbackIsNil(t, p) {
					t.Error("search SoundCloud adapter lost its yt-dlp fallback")
				}
			}
			assertEqual(t, "search providers", search, tt.search)

			var consens []string
			for _, p := range BuildConsensusProviders(cfg, nil) {
				consens = append(consens, p.Name)
			}
			assertEqual(t, "consensus providers", consens, tt.consens)

			var artwork []string
			resolvers := reflect.ValueOf(buildArtworkChain(newClientFactory(nil), cfg)).Elem().FieldByName("resolvers")
			for i := range resolvers.Len() {
				artwork = append(artwork, typeName(resolvers.Index(i)))
			}
			assertEqual(t, "artwork chain", artwork, tt.artwork)
		})
	}
}

func assertEqual(t *testing.T, what string, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("%s =\n  %v\nwant\n  %v", what, got, want)
	}
}

// TestScrapedProviderBuilders pins that each per-provider builder applies its
// kill switch internally: nil when disabled, a live adapter when enabled.
func TestScrapedProviderBuilders(t *testing.T) {
	off, on := scrapedProvidersConfig(false), scrapedProvidersConfig(true)
	cf := newClientFactory(nil)

	if buildAppleMusicAdapter(cf, off) != nil || buildAppleMusicAdapter(cf, on) == nil {
		t.Error("buildAppleMusicAdapter does not follow the Apple Music kill switch")
	}
	if buildSpotifyAdapter(cf, off) != nil || buildSpotifyAdapter(cf, on) == nil {
		t.Error("buildSpotifyAdapter does not follow the Spotify kill switch")
	}
	if buildSoundCloudAdapter(cf, off) != nil || buildSoundCloudAdapter(cf, on) == nil {
		t.Error("buildSoundCloudAdapter does not follow the SoundCloud kill switch")
	}
	if buildSoundCloudSearchAdapter(cf, off) != nil || buildSoundCloudSearchAdapter(cf, on) == nil {
		t.Error("buildSoundCloudSearchAdapter does not follow the SoundCloud kill switch")
	}
	if !soundCloudFallbackIsNil(t, buildSoundCloudAdapter(cf, on)) {
		t.Error("buildSoundCloudAdapter must build without a search fallback")
	}
	if soundCloudFallbackIsNil(t, buildSoundCloudSearchAdapter(cf, on)) {
		t.Error("buildSoundCloudSearchAdapter must carry the yt-dlp search fallback")
	}
}
