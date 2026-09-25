package app

import (
	discoveryPersistence "altune/go-api/internal/discovery/adapters/persistence"
	discoveryDomain "altune/go-api/internal/discovery/domain"
	discoveryService "altune/go-api/internal/discovery/service"
	"altune/go-api/internal/shared/config"
	"fmt"
	"reflect"
	"runtime"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
)

// TestSearchServiceOptionSets pins the ordered option set each named search
// constructor composes, captured from the former rankingOnly=false/true paths,
// so the split into named constructors stays behavior-preserving.
func TestSearchServiceOptionSets(t *testing.T) {
	cfg := &config.Config{
		ExplorationEnabled:         true,
		TailDemotionEnabled:        true,
		CrossKindProminenceEnabled: true,
		IdentityVerifyOnPersist:    true,
		BehavioralRankingEnabled:   true,
		MusicBrainzUserAgent:       "altune-test",
	}
	pool := &pgxpool.Pool{}
	redisClient := goredis.NewClient(&goredis.Options{Addr: "127.0.0.1:1"})
	t.Cleanup(func() { _ = redisClient.Close() })
	eventStore := discoveryPersistence.NewPgxEventStore(pool)
	w := newSearchWiring(cfg, nil)

	tests := []struct {
		name string
		opts []discoveryService.Option
		want []string
	}{
		{
			name: "content with redis",
			opts: contentServiceOptions(w, cfg, pool, redisClient, eventStore, nil),
			want: []string{
				"WithHistoryRepository", "WithTailDemotion", "WithCrossKindProminence", "WithExploration",
				"WithArtworkResolver", "WithFindRelatedService", "WithFavorites", "WithIdentityStore",
				"WithIdentityVerifier", "WithResultCache", "WithHeldSlateCache", "WithArtworkCache",
				"WithIdentityBridge", "WithMBIDIndex", "WithVocabularyStore", "WithEventStore",
				"WithBehavioralRanking", "WithAlbumValidator",
			},
		},
		{
			name: "content without redis",
			opts: contentServiceOptions(w, cfg, pool, nil, eventStore, nil),
			want: []string{
				"WithHistoryRepository", "WithTailDemotion", "WithCrossKindProminence", "WithExploration",
				"WithArtworkResolver", "WithFindRelatedService", "WithFavorites", "WithIdentityStore",
				"WithIdentityVerifier", "WithEventStore", "WithBehavioralRanking", "WithAlbumValidator",
			},
		},
		{
			name: "ranking only with redis",
			opts: rankingOnlyServiceOptions(w, cfg, pool, redisClient),
			want: []string{
				"WithHistoryRepository", "WithTailDemotion", "WithCrossKindProminence",
				"WithArtworkCache", "WithIdentityBridge", "WithMBIDIndex", "WithVocabularyStore",
				"WithAlbumValidator",
			},
		},
		{
			name: "ranking only without redis",
			opts: rankingOnlyServiceOptions(w, cfg, pool, nil),
			want: []string{
				"WithHistoryRepository", "WithTailDemotion", "WithCrossKindProminence", "WithAlbumValidator",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := optionNames(tt.opts); !slices.Equal(got, tt.want) {
				t.Errorf("options =\n  %v\nwant\n  %v", got, tt.want)
			}
		})
	}
}

// optionNames resolves each option closure to the discovery With* constructor
// that produced it. The runtime name depends on inlining (plain builds yield
// "service.WithX.func1", coverage builds "app.caller.WithX.func2"), so take the
// last dot-separated segment that names a With* constructor.
func optionNames(opts []discoveryService.Option) []string {
	names := make([]string, 0, len(opts))
	for _, opt := range opts {
		full := runtime.FuncForPC(reflect.ValueOf(opt).Pointer()).Name()
		names = append(names, lastWithSegment(full))
	}
	return names
}

func lastWithSegment(funcName string) string {
	segments := strings.Split(funcName, ".")
	for i := len(segments) - 1; i >= 0; i-- {
		if strings.HasPrefix(segments[i], "With") {
			return segments[i]
		}
	}
	return funcName
}

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
