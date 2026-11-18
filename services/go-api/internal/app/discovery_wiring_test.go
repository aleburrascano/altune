package app

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/observe"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/events"
	shared "altune/go-api/internal/shared/kernel"
	"altune/go-api/internal/shared/runloop"
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

type countingProviderRT struct {
	mu    sync.Mutex
	calls int
}

func (rt *countingProviderRT) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.mu.Lock()
	rt.calls++
	rt.mu.Unlock()
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader("{}")),
		Request:    req,
	}, nil
}

func (rt *countingProviderRT) roundTrips() int {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.calls
}

func totalProviderCounts(s discovery.ProviderMetricsSnapshot) int64 {
	var total int64
	for _, o := range s {
		total += o.OK + o.Quota + o.Error
	}
	return total
}

func TestRequestPathProviderCallsAreCountedOnce(t *testing.T) {
	routes := []struct {
		name   string
		target string
	}{
		{"album content", "/albums/deezer/1/tracks"},
		{"lyrics", "/lyrics?title=song&subtitle=artist"},
		{"enrichment", "/enrichment?kind=album&title=album"},
	}

	for _, route := range routes {
		t.Run(route.name, func(t *testing.T) {
			rt := &countingProviderRT{}
			a := &App{cfg: &config.Config{MusicBrainzUserAgent: "altune-test/1.0"}}
			disc, err := discovery.New(a.discoveryDeps(context.Background(), newClientFactory(countedProviderTransport(rt))))
			if err != nil {
				t.Fatalf("wire discovery: %v", err)
			}
			before := discovery.ReadProviderMetricsSnapshot()
			req := httptest.NewRequest(http.MethodGet, route.target, nil)
			req = req.WithContext(auth.ContextWithUserID(req.Context(), shared.NewUserId(uuid.New())))
			disc.Handler.Routes().ServeHTTP(httptest.NewRecorder(), req)
			counted := totalProviderCounts(discovery.ReadProviderMetricsSnapshot()) - totalProviderCounts(before)

			if rt.roundTrips() == 0 {
				t.Fatalf("%s made no provider call, so it proves nothing about the transport", route.target)
			}
			if counted != int64(rt.roundTrips()) {
				t.Errorf("provider counters moved by %d over %d provider calls, want one count per call", counted, rt.roundTrips())
			}
		})
	}
}

type enrichRealChainFakeEnricher struct {
	enrichment discovery.MBEnrichment
}

func (f *enrichRealChainFakeEnricher) ResolveMBID(_ context.Context, _ discovery.ResultKind, _, _ string) (string, error) {
	return "", nil
}

func (f *enrichRealChainFakeEnricher) Lookup(_ context.Context, _ discovery.ResultKind, _ string) (discovery.MBEnrichment, error) {
	return f.enrichment, nil
}

type enrichRealChainMemCache struct {
	pos map[string]discovery.MBEnrichment
	neg map[string]bool
}

func newEnrichRealChainMemCache() *enrichRealChainMemCache {
	return &enrichRealChainMemCache{pos: map[string]discovery.MBEnrichment{}, neg: map[string]bool{}}
}

func (c *enrichRealChainMemCache) Get(_ context.Context, kind discovery.ResultKind, mbid string) (discovery.MBEnrichment, bool, error) {
	e, ok := c.pos[kind.String()+"|"+mbid]
	return e, ok, nil
}

func (c *enrichRealChainMemCache) Set(_ context.Context, kind discovery.ResultKind, mbid string, e discovery.MBEnrichment) error {
	c.pos[kind.String()+"|"+mbid] = e
	return nil
}

func (c *enrichRealChainMemCache) GetNegative(_ context.Context, kind discovery.ResultKind, nameKey string) (bool, error) {
	return c.neg[kind.String()+"|"+nameKey], nil
}

func (c *enrichRealChainMemCache) SetNegative(_ context.Context, kind discovery.ResultKind, nameKey string) error {
	c.neg[kind.String()+"|"+nameKey] = true
	return nil
}

func enrichRealChainSampleEnrichment() discovery.MBEnrichment {
	e := discovery.EmptyEnrichment()
	e.MBID = "mbid-1"
	e.Genres = []string{"hip hop"}
	e.Year = 2017
	return e
}

type enrichTestHostRewriteTransport struct {
	targetURL string
}

func (t *enrichTestHostRewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme = "http"
	req.URL.Host = strings.TrimPrefix(t.targetURL, "http://")
	return http.DefaultTransport.RoundTrip(req)
}

func rewrittenClient(targetURL string) *http.Client {
	return &http.Client{Transport: &enrichTestHostRewriteTransport{targetURL: targetURL}}
}

func TestEnrichmentService_CallerSuppliedMBID_RealChainReachesCoverArtArchive(t *testing.T) {
	caaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Errorf("method = %q, want HEAD", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer caaServer.Close()

	caa := discovery.NewCoverArtArchiveResolver(rewrittenClient(caaServer.URL))
	chain := discovery.NewChainedArtworkResolver(caa, discovery.NewCoverArtArchiveIdentityResolver(caa))

	enr := &enrichRealChainFakeEnricher{enrichment: enrichRealChainSampleEnrichment()}
	svc := discovery.NewEnrichmentService(enr, chain, newEnrichRealChainMemCache())

	got, err := svc.Execute(context.Background(), discovery.ResultKindAlbum,
		"Some Other Album", "Other Artist", "mbid-1")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := "https://coverartarchive.org/release-group/mbid-1/front-1200"
	if got.ArtworkURL != want {
		t.Errorf("artwork_url = %q, want the real chain's mbid-native CoverArtArchive answer %q — "+
			"the caller-supplied-mbid path must still reach a title-blind provider instead of a "+
			"verified-empty miss that would poison the shared cache with a blank cover", got.ArtworkURL, want)
	}
}

func TestEnrichmentService_CallerSuppliedMBID_RealChainForwardsBridgedExternalIDs(t *testing.T) {
	discogsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/artists/38" {
			t.Errorf("path = %q, want the exact bridged discogs id, no name search", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id": 38, "images": [{"type": "primary", "uri": "https://img/real-cover.jpg"}]}`))
	}))
	defer discogsServer.Close()

	discogsAdapter := discovery.NewDiscogsAdapter(rewrittenClient(discogsServer.URL), "test-token", "altune-test/1.0")
	chain := discovery.NewChainedArtworkResolver(discogsAdapter)

	e := discovery.EmptyEnrichment()
	e.MBID = "mbid-artist-1"
	e.ExternalIDs = map[discovery.ProviderKey]string{"discogs": "38"}
	enr := &enrichRealChainFakeEnricher{enrichment: e}
	svc := discovery.NewEnrichmentService(enr, chain, newEnrichRealChainMemCache())

	got, err := svc.Execute(context.Background(), discovery.ResultKindArtist,
		"Some Other Artist Name", "", "mbid-artist-1")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got.ArtworkURL != "https://img/real-cover.jpg" {
		t.Errorf("artwork_url = %q, want the bridged Discogs id's real cover — "+
			"the caller-supplied-mbid path must forward the MB-known ExternalIDs, not resolve on mbid alone",
			got.ArtworkURL)
	}
}

type failingBehavioralStore struct{ err error }

func (s failingBehavioralStore) SatisfactionSignals(context.Context, time.Time) ([]discovery.BehavioralSignal, error) {
	return nil, s.err
}

func TestBehavioralRankingRefresh_LogsOnFailure(t *testing.T) {
	var buf bytes.Buffer
	restore := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	defer slog.SetDefault(restore)

	boom := errors.New("boom")
	consumer := discovery.NewSatisfactionConsumer(failingBehavioralStore{err: boom})
	searchSvc := discovery.NewSearchService(nil, nil, discovery.WithBehavioralRanking(consumer))

	a := &App{cfg: &config.Config{BehavioralRankingEnabled: true}}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); a.wg.Wait() })

	a.startDiscoveryBackgroundJobs(ctx, &discovery.Discovery{Search: searchSvc})

	h := waitForHealth(t, a, jobBehavioralRankingRefresh, func(h runloop.JobHealth) bool { return h.Failures >= 1 })

	if h.LastFailure.IsZero() {
		t.Error("a failed run must still update LastFailure through jc.record")
	}

	logged := buf.String()
	wantFailed := `level=WARN msg="behavioral ranking refresh failed" error=boom` + "\n"
	if !strings.Contains(logged, wantFailed) {
		t.Errorf("failure line drifted: want %q in\n%s", wantFailed, logged)
	}
}

func TestUnreleasedRefresh_AStoredUnreleasedServiceRegistersTheLeaderOnlyJob(t *testing.T) {
	a := &App{cfg: &config.Config{}}
	a.startUnreleasedRefresh(&discovery.UnreleasedService{})
	if got := startedJobNames(a); !slices.Equal(got, []jobName{jobUnreleasedRefresh}) {
		t.Errorf("leader-gated jobs = %v, want [%s]", got, jobUnreleasedRefresh)
	}
}

func TestUnreleasedRefresh_NothingStoredStartsNoJob(t *testing.T) {
	a := &App{cfg: &config.Config{}}
	a.startUnreleasedRefresh(nil)
	if got := startedJobNames(a); len(got) != 0 {
		t.Errorf("no stored unreleased service started jobs %v", got)
	}
}

func TestUnreleasedRefresh_DisabledJobsAcceptsTheJobName(t *testing.T) {
	a := &App{cfg: &config.Config{DisabledJobs: []string{string(jobUnreleasedRefresh)}}}
	if err := a.applyStartupSwitches(t.Context()); err != nil {
		t.Fatalf("applyStartupSwitches: %v", err)
	}
	if h := findJobHealth(t, a.JobHealth(), "unreleased refresh"); h.Enabled {
		t.Error("unreleased refresh enabled, want disabled via DISABLED_JOBS")
	}
}

type limitRecordingStore struct {
	discovery.FakeUnreleasedStore
	limits []int
}

func (l *limitRecordingStore) Due(_ context.Context, _ time.Time, limit int) ([]discovery.UnreleasedDueArtist, error) {
	l.limits = append(l.limits, limit)
	return nil, nil
}

func TestUnreleasedRefresh_EachRunAsksTheStoreForAtMostTwoArtists(t *testing.T) {
	store := &limitRecordingStore{}
	svc := discovery.NewUnreleasedService(nil, nil, nil, discovery.UnreleasedConfig{}, discovery.WithUnreleasedStore(store))
	if err := unreleasedRefreshRun(svc)(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !slices.Equal(store.limits, []int{2}) {
		t.Errorf("Due limits = %v, want [2]", store.limits)
	}
}

func TestUnreleasedRefresh_RegistrationLogsAThirtyMinuteInterval(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	a := &App{cfg: &config.Config{}}
	a.startUnreleasedRefresh(&discovery.UnreleasedService{})
	if !strings.Contains(buf.String(), "interval=30m0s") {
		t.Errorf("registration log lacks interval=30m0s: %s", buf.String())
	}
}

func distinctDiscoveryConfig() *config.Config {
	return &config.Config{
		MusicBrainzUserAgent:              "mb-agent",
		LastFMAPIKey:                      "lastfm-key",
		DiscogsToken:                      "discogs-token",
		FanartTVAPIKey:                    "fanart-key",
		GeniusAccessToken:                 "genius-token",
		TheAudioDBAPIKey:                  "theaudiodb-key",
		GoogleSheetsAPIKey:                "sheets-key",
		TrackerHubSheetID:                 "hub-sheet",
		DiscoverySearchProviders:          []string{"deezer", "spotify"},
		DiscoverySearchDeadline:           1 * time.Second,
		ExplorationRate:                   0.25,
		DiscoveryProviderCacheTTL:         2 * time.Minute,
		DiscoveryMusicBrainzCacheTTL:      3 * time.Minute,
		GeniusUnreleasedMaxPages:          4,
		GeniusUnreleasedMaxDetails:        5,
		GeniusUnreleasedDetailConcurrency: 6,
		GeniusUnreleasedTTL:               7 * time.Hour,
		GeniusUnreleasedEmptyTTL:          8 * time.Hour,
		GeniusUnreleasedWait:              9 * time.Second,
		GeniusUnreleasedJobTimeout:        10 * time.Second,
	}
}

func TestDiscoveryDeps_CopiesEachConfigValueIntoItsOwnField(t *testing.T) {
	a := &App{cfg: distinctDiscoveryConfig()}
	deps := a.discoveryDeps(t.Context(), newClientFactory(http.DefaultTransport))

	wantProviders := discovery.ProviderSettings{
		MusicBrainzUserAgent: "mb-agent",
		LastFMAPIKey:         "lastfm-key",
		DiscogsToken:         "discogs-token",
		FanartTVAPIKey:       "fanart-key",
		GeniusAccessToken:    "genius-token",
		TheAudioDBAPIKey:     "theaudiodb-key",
		GoogleSheetsAPIKey:   "sheets-key",
		TrackerHubSheetID:    "hub-sheet",
		SearchAllowlist:      []string{"deezer", "spotify"},
	}
	if !reflect.DeepEqual(deps.Providers, wantProviders) {
		t.Errorf("providers =\n  %+v\nwant\n  %+v", deps.Providers, wantProviders)
	}
	wantSearch := discovery.SearchSettings{
		Deadline:            1 * time.Second,
		ExplorationRate:     0.25,
		ProviderCacheTTL:    2 * time.Minute,
		MusicBrainzCacheTTL: 3 * time.Minute,
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

func TestDiscoveryDeps_EachConfigSwitchTurnsOnOnlyItsOwnSetting(t *testing.T) {
	tests := []struct {
		name           string
		turnOn         func(*config.Config)
		wantProviders  discovery.ProviderSettings
		wantSearch     discovery.SearchSettings
		wantUnreleased discovery.UnreleasedSettings
	}{
		{"AppleMusicEnabled", func(c *config.Config) { c.AppleMusicEnabled = true }, discovery.ProviderSettings{AppleMusicEnabled: true}, discovery.SearchSettings{}, discovery.UnreleasedSettings{}},
		{"SpotifyEnabled", func(c *config.Config) { c.SpotifyEnabled = true }, discovery.ProviderSettings{SpotifyEnabled: true}, discovery.SearchSettings{}, discovery.UnreleasedSettings{}},
		{"SoundCloudEnabled", func(c *config.Config) { c.SoundCloudEnabled = true }, discovery.ProviderSettings{SoundCloudEnabled: true}, discovery.SearchSettings{}, discovery.UnreleasedSettings{}},
		{"AmazonMusicEnabled", func(c *config.Config) { c.AmazonMusicEnabled = true }, discovery.ProviderSettings{AmazonMusicEnabled: true}, discovery.SearchSettings{}, discovery.UnreleasedSettings{}},
		{"YtMusicEnabled", func(c *config.Config) { c.YtMusicEnabled = true }, discovery.ProviderSettings{YouTubeMusicEnabled: true}, discovery.SearchSettings{}, discovery.UnreleasedSettings{}},
		{"TailDemotionEnabled", func(c *config.Config) { c.TailDemotionEnabled = true }, discovery.ProviderSettings{}, discovery.SearchSettings{TailDemotion: true}, discovery.UnreleasedSettings{}},
		{"CrossKindProminenceEnabled", func(c *config.Config) { c.CrossKindProminenceEnabled = true }, discovery.ProviderSettings{}, discovery.SearchSettings{CrossKindProminence: true}, discovery.UnreleasedSettings{}},
		{"ExplorationEnabled", func(c *config.Config) { c.ExplorationEnabled = true }, discovery.ProviderSettings{}, discovery.SearchSettings{Exploration: true}, discovery.UnreleasedSettings{}},
		{"IdentityVerifyOnPersist", func(c *config.Config) { c.IdentityVerifyOnPersist = true }, discovery.ProviderSettings{}, discovery.SearchSettings{IdentityVerifyOnPersist: true}, discovery.UnreleasedSettings{}},
		{"BehavioralRankingEnabled", func(c *config.Config) { c.BehavioralRankingEnabled = true }, discovery.ProviderSettings{}, discovery.SearchSettings{BehavioralRanking: true}, discovery.UnreleasedSettings{}},
		{"UnreleasedSource=cache", func(c *config.Config) { c.UnreleasedSource = "cache" }, discovery.ProviderSettings{}, discovery.SearchSettings{}, discovery.UnreleasedSettings{ServeFromCache: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			tt.turnOn(cfg)
			deps := (&App{cfg: cfg}).discoveryDeps(t.Context(), newClientFactory(http.DefaultTransport))
			if !reflect.DeepEqual(deps.Providers, tt.wantProviders) || deps.Search != tt.wantSearch || deps.Unreleased != tt.wantUnreleased {
				t.Errorf("deps =\n  %+v\n  %+v\n  %+v\nwant\n  %+v\n  %+v\n  %+v",
					deps.Providers, deps.Search, deps.Unreleased, tt.wantProviders, tt.wantSearch, tt.wantUnreleased)
			}
		})
	}
}

func TestDiscoveryDeps_PassesTheAppsOwnHandles(t *testing.T) {
	a := &App{cfg: &config.Config{}, eventTap: observe.NewEventTap(events.NewInProcessBus())}
	ctx := t.Context()
	transport := &stubTransport{}
	deps := a.discoveryDeps(ctx, newClientFactory(transport))

	if deps.Lifecycle != ctx || deps.Background != &a.wg || deps.Transport != transport || deps.CacheOps != cacheOps() {
		t.Error("deps do not carry the app's lifecycle, wait group, transport and cache counters")
	}
	if deps.ProviderTimeout != 10*time.Second || deps.ChartTimeout != 15*time.Second {
		t.Errorf("timeouts = %v, %v, want 10s and 15s", deps.ProviderTimeout, deps.ChartTimeout)
	}
	if deps.Activity != a.eventTap {
		t.Error("deps do not feed discovery's activity into the app's event tap")
	}
	if (&App{cfg: &config.Config{}}).discoveryDeps(ctx, newClientFactory(transport)).Activity != nil {
		t.Error("an app without an event tap handed discovery a non-nil activity feed")
	}
}

type stubTransport struct{}

func (*stubTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("stub transport")
}
