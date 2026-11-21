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
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
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

func TestDiscoveryDeps_CopiesEachConfigValueIntoItsOwnField(t *testing.T) {
	a := &App{cfg: distinctDiscoveryConfig()}
	deps := a.discoveryDeps(t.Context(), newClientFactory(http.DefaultTransport))

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

func TestDiscoveryDeps_EachConfigSwitchTurnsOnOnlyItsOwnSetting(t *testing.T) {
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
			deps := (&App{cfg: cfg}).discoveryDeps(t.Context(), newClientFactory(http.DefaultTransport))
			if !reflect.DeepEqual(deps.Providers, tt.wantProviders) || deps.Search != tt.wantSearch || deps.Unreleased != tt.wantUnreleased {
				t.Errorf("deps =\n  %+v\n  %+v\n  %+v\nwant\n  %+v\n  %+v\n  %+v",
					deps.Providers, deps.Search, deps.Unreleased, tt.wantProviders, tt.wantSearch, tt.wantUnreleased)
			}
		})
	}
}

func TestDiscoveryDeps_PassesTheAppsOwnHandles(t *testing.T) {
	a := &App{cfg: &config.Config{}, eventTap: observe.NewEventTap(events.NewInProcessBus(time.Now, filepath.Join(t.TempDir(), "event-id-floor")))}
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

func TestDiscoveryDeps_ClientEventsFollowTheTelemetryExportSetting(t *testing.T) {
	transport := newClientFactory(&stubTransport{})
	if deps := (&App{cfg: &config.Config{}}).discoveryDeps(t.Context(), transport); deps.ClientEvents != nil {
		t.Error("an app with no OTLP endpoint handed discovery client events to export")
	}
	exporting := &App{cfg: &config.Config{OTLPEndpoint: "https://otlp.example.com"}}
	if deps := exporting.discoveryDeps(t.Context(), transport); deps.ClientEvents == nil {
		t.Error("an app with an OTLP endpoint handed discovery no client events")
	}
}

type stubTransport struct{}

func (*stubTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("stub transport")
}

type hostRecordingTransport struct {
	mu    sync.Mutex
	hosts []string
}

func (rt *hostRecordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.hosts = append(rt.hosts, req.URL.Host)
	return nil, errors.New("recorded only")
}

func (rt *hostRecordingTransport) asked() []string {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return slices.Clone(rt.hosts)
}

func TestNewEvalDiscovery_CallsProvidersThroughTheGivenTransportAndIdentityStore(t *testing.T) {
	transport := &hostRecordingTransport{}
	store := &discovery.FakeIdentityStore{Mbid: "mbid-1", Xref: map[discovery.ProviderKey]string{"spotify": "4"}}

	disc, err := NewEvalDiscovery(t.Context(), &config.Config{}, EvalHandles{Transport: transport, IdentityStore: store})
	if err != nil {
		t.Fatalf("NewEvalDiscovery: %v", err)
	}
	_, _ = disc.EvalArtistContent().GetTopTracks(t.Context(), discovery.ProviderDeezer, "1", nil, 10)
	if !slices.Contains(transport.asked(), "api.deezer.com") || !slices.Contains(transport.asked(), "open.spotify.com") {
		t.Errorf("asked %v, want Deezer and the Spotify id bridged by the given identity store", transport.asked())
	}
}

func TestNewEvalDiscovery_MapsTheConfigIntoTheProviderTable(t *testing.T) {
	disc, err := NewEvalDiscovery(t.Context(), &config.Config{MusicBrainzUserAgent: "altune-test"}, EvalHandles{Transport: &stubTransport{}})
	if err != nil {
		t.Fatalf("NewEvalDiscovery: %v", err)
	}
	var consensus []string
	for _, source := range disc.Consensus {
		consensus = append(consensus, source.Name)
	}
	if !slices.Contains(consensus, "musicbrainz") {
		t.Errorf("consensus = %v, want MusicBrainz once its user agent is configured", consensus)
	}
}

func TestNewEvalDiscovery_SearchesWithTheGivenPoolAndRedis(t *testing.T) {
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	pool := unreachableEvalPool(t)
	redisClient := goredis.NewClient(&goredis.Options{Addr: miniredis.RunT(t).Addr()})

	disc, err := NewEvalDiscovery(t.Context(), &config.Config{}, EvalHandles{Pool: pool, Redis: redisClient, Transport: &stubTransport{}})
	if err != nil {
		t.Fatalf("NewEvalDiscovery: %v", err)
	}
	disc.EvalSearch()
	if !strings.Contains(logs.String(), "favorites") || disc.Vocabulary == nil {
		t.Errorf("eval discovery ignored the given pool or redis; logs:\n%s", logs.String())
	}
}

func unreachableEvalPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), "postgres://u:p@127.0.0.1:1/altune")
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}
