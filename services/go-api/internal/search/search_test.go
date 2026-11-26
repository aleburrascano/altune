package search_test

import (
	"altune/go-api/internal/search"
	"altune/go-api/internal/search/searchtest"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/requestuser"
	"altune/go-api/internal/shared/testdb"
	"bytes"
	"context"
	"expvar"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func searchOrder(t *testing.T, h *harness, disc *search.Discovery) []string {
	t.Helper()
	query := &search.SearchQuery{Raw: "probe song", Kinds: map[search.ResultKind]bool{search.ResultKindAlbum: true}}
	_, statuses := disc.Search.InspectSearchWithStatuses(context.Background(), query)
	h.deps.Tracker.Wait()
	var names []string
	for _, status := range statuses {
		names = append(names, status.Provider.String())
	}
	return names
}

func TestSearch_AsksTheProvidersInTableOrder(t *testing.T) {
	tests := []struct {
		name      string
		providers func() search.ProviderSettings
		want      []string
	}{
		{"every key set", everyProviderOn, []string{"deezer", "applemusic", "soundcloud", "youtube", "spotify"}},
		{"prod-like", prodLikeProviders, []string{"deezer", "applemusic", "soundcloud", "youtube", "spotify"}},
		{"every key missing", everyProviderOff, []string{"deezer", "applemusic", "soundcloud", "youtube", "spotify"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, tt.providers())
			assertLines(t, searchOrder(t, h, h.start()), tt.want)
		})
	}
}

func TestSearch_SoundCloudFallsBackToYtDlpWhenItsAPIFails(t *testing.T) {
	bin := t.TempDir()
	marker := filepath.Join(bin, "called")
	script := "#!/bin/sh\necho called >> \"" + marker + "\"\nexit 1\n"
	if err := os.WriteFile(filepath.Join(bin, "yt-dlp"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	providers := everyProviderOff()
	h := newHarness(t, providers)
	disc := h.start()

	query := &search.SearchQuery{Raw: "probe song", Kinds: map[search.ResultKind]bool{search.ResultKindTrack: true}}
	disc.Search.InspectSearchWithStatuses(context.Background(), query)
	h.deps.Tracker.Wait()

	if _, err := os.Stat(marker); err != nil {
		t.Error("SoundCloud search never fell back to yt-dlp after its API failed")
	}
}

func TestProviderCalls_UseTheProviderTimeoutAndChartsTheChartTimeout(t *testing.T) {
	h := newHarness(t, prodLikeProviders())
	disc := h.start()

	serve(disc, "/albums/deezer/1/tracks")
	if got := h.transport.timeoutsByHost()["api.deezer.com"]; got != 10*time.Second {
		t.Errorf("album call timeout = %v, want the 10s provider timeout", got)
	}
	var charts []string
	for _, chart := range disc.Charts {
		charts = append(charts, chart.Name().String())
		_, _ = chart.FetchCharts(context.Background(), 1)
	}
	assertLines(t, charts, []string{"deezer"})
	for host, timeout := range h.transport.timeoutsByHost() {
		if timeout != 15*time.Second {
			t.Errorf("chart call to %s timed out at %v, want the 15s chart timeout", host, timeout)
		}
	}
}

func TestCharts_AreDeezerAloneEvenWithEveryKeyMissing(t *testing.T) {
	disc := newHarness(t, everyProviderOff()).start()

	if len(disc.Charts) != 1 || disc.Charts[0].Name() != search.ProviderDeezer {
		t.Errorf("charts = %v, want Deezer alone", disc.Charts)
	}
}

func TestEnrichment_AsksArtworkProvidersInChainOrder(t *testing.T) {
	h := newHarness(t, everyProviderOn())
	disc := h.start()

	serve(disc, "/enrichment?kind=artist&title=Probe+Artist")

	var asked []string
	for _, call := range h.transport.recorded() {
		asked = append(asked, call.host+call.path)
	}
	assertLines(t, asked, []string{
		"musicbrainz.org/ws/2/artist/",
		"musicbrainz.org/ws/2/artist/" + probeArtistMBID,
		"webservice.fanart.tv/v3/music/" + probeArtistMBID,
		"api.genius.com/search",
		"theaudiodb.com/api/v1/json/theaudiodb-key/artist-mb.php",
		"theaudiodb.com/api/v1/json/theaudiodb-key/search.php",
		"api.deezer.com/search/artist",
		"itunes.apple.com/search",
		"music.youtube.com/youtubei/v1/search",
		"soundcloud.com/",
	})
	if got := h.transport.timeoutsByHost()["music.youtube.com"]; got != 8*time.Second {
		t.Errorf("YouTube Music artwork timeout = %v, want its own 8s over the raw transport", got)
	}
}

func TestContent_EachSwitchedOnProviderServesItsJobs(t *testing.T) {
	tests := []struct {
		target string
		host   string
	}{
		{"/albums/applemusic/1/tracks", "music.apple.com"},
		{"/albums/itunes/1/tracks", "itunes.apple.com"},
		{"/albums/spotify/1/tracks", "open.spotify.com"},
		{"/albums/soundcloud/1/tracks", "soundcloud.com"},
		{"/artists/spotify/1/content", "open.spotify.com"},
		{"/tracks/soundcloud/1/related", "soundcloud.com"},
		{"/enrichment/deezer?kind=track&title=Probe+Song&subtitle=Probe+Artist", "api.deezer.com"},
		{"/lyrics?title=Probe+Song&subtitle=Probe+Artist", "api.deezer.com"},
	}
	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			on := newHarness(t, everyProviderOn())
			serve(on.start(), tt.target)
			if !slices.Contains(on.transport.hosts(), tt.host) {
				t.Errorf("%s never reached %s with every provider on; asked %v", tt.target, tt.host, on.transport.hosts())
			}
		})
	}
}

func TestEnrichment_WithoutAMusicBrainzUserAgentAsksNoProvider(t *testing.T) {
	off := newHarness(t, everyProviderOff())

	serve(off.start(), "/enrichment?kind=artist&title=Probe+Artist")
	if hosts := off.transport.hosts(); len(hosts) != 0 {
		t.Errorf("enrichment reached %v without a MusicBrainz user agent", hosts)
	}
}

func TestFeaturedArtists_WithoutMusicBrainzAskOnlyDeezer(t *testing.T) {
	h := newHarness(t, everyProviderOff())
	disc := h.start()

	_, _ = disc.FeaturedArtists.Resolve(context.Background(), "Probe Artist", "Probe Song")
	assertLines(t, h.transport.hosts(), []string{"api.deezer.com"})
}

func TestFeaturedArtists_AskMusicBrainzFirstWhenItIsOn(t *testing.T) {
	h := newHarness(t, prodLikeProviders())
	disc := h.start()

	_, _ = disc.FeaturedArtists.Resolve(context.Background(), "Probe Artist", "Probe Song")
	assertLines(t, h.transport.hosts(), []string{"musicbrainz.org", "api.deezer.com"})
}

func TestRecordings_AreMusicBrainzAndAbsentWithoutIt(t *testing.T) {
	if disc := newHarness(t, everyProviderOff()).start(); disc.Recordings != nil {
		t.Errorf("recordings = %#v without a MusicBrainz user agent, want a nil interface", disc.Recordings)
	}
	h := newHarness(t, prodLikeProviders())
	disc := h.start()
	if disc.Recordings == nil {
		t.Fatal("no recording authority although MusicBrainz is on")
	}
	_, _ = disc.Recordings.RecordingsByISRC(context.Background(), "USRC17607839")
	if got := h.transport.timeoutsByHost()["musicbrainz.org"]; got != 10*time.Second {
		t.Errorf("recording lookup timeout = %v, want the 10s provider timeout", got)
	}
}

func TestUnreleased_AsksOnlyTheSourcesThatAreOn(t *testing.T) {
	sheetsOnly := everyProviderOff()
	sheetsOnly.GoogleSheetsAPIKey = "sheets-key"
	geniusOnly := everyProviderOff()
	geniusOnly.GeniusAccessToken = "genius-token"
	tests := []struct {
		name      string
		providers search.ProviderSettings
		want      []string
	}{
		{"sheets key alone", sheetsOnly, []string{"api.deezer.com", "sheets.googleapis.com"}},
		{"genius token alone", geniusOnly, []string{"api.deezer.com", "api.genius.com"}},
		{"neither", everyProviderOff(), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, tt.providers)
			serve(h.start(), "/artists/deezer/1/unreleased?name=Probe+Artist")
			hosts := h.transport.hosts()
			slices.Sort(hosts)
			assertLines(t, slices.Compact(hosts), tt.want)
		})
	}
}

func unreachablePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), "postgres://u:p@127.0.0.1:1/altune")
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestUnreleasedRefresh_OnlyAStoreBackedServiceNeedsRefreshing(t *testing.T) {
	tests := []struct {
		name      string
		fromCache bool
		pool      bool
		want      bool
	}{
		{"store source with a pool", false, true, true},
		{"cache source", true, true, false},
		{"store source without a pool", false, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, prodLikeProviders())
			h.deps.Unreleased.ServeFromCache = tt.fromCache
			if tt.pool {
				h.deps.Pool = unreachablePool(t)
			}
			if got := h.start().UnreleasedRefresh != nil; got != tt.want {
				t.Errorf("unreleased refresh present = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUnreleasedRefresh_WithoutAnyUnreleasedSourceThereIsNothingToRefresh(t *testing.T) {
	h := newHarness(t, everyProviderOff())
	h.deps.Pool = unreachablePool(t)

	if h.start().UnreleasedRefresh != nil {
		t.Error("an unreleased refresh exists with neither a Genius token nor a Sheets key")
	}
}

func TestUnreleased_StoreSourceWithoutAPoolWarnsOnceThroughTheGivenLogger(t *testing.T) {
	h := newHarness(t, prodLikeProviders())
	h.start()

	warnings := h.logLines("unreleased.store_unavailable_using_cache")
	if len(warnings) != 1 || !strings.Contains(warnings[0], `level=WARN`) || !strings.Contains(warnings[0], `reason="no database pool"`) {
		t.Errorf("fallback warnings = %v, want exactly one WARN naming the missing pool", warnings)
	}
}

func TestVocabulary_IsAbsentWithoutRedis(t *testing.T) {
	if disc := newHarness(t, prodLikeProviders()).start(); disc.SpellingHints != nil {
		t.Errorf("vocabulary store = %#v without redis, want a nil interface", disc.SpellingHints)
	}
}

func searchPage(disc *search.Discovery) int {
	return serve(disc, "/search?q=probe+album&kinds=album&save_history=false")
}

func TestSearch_WithoutADatabaseOrRedisStillAnswers(t *testing.T) {
	h := newHarness(t, everyProviderOn())

	if code := searchPage(h.start()); code != http.StatusOK {
		t.Errorf("search without a database or redis answered %d, want 200", code)
	}
}

func TestSearch_WithRedisARepeatedSearchDoesNotAskAProviderThatAnswered(t *testing.T) {
	h := newHarness(t, everyProviderOff())
	h.deps.Redis = goredis.NewClient(&goredis.Options{Addr: miniredis.RunT(t).Addr()})
	disc := h.start()

	searchPage(disc)
	h.deps.Tracker.Wait()
	asked := len(h.transport.recorded())
	if code := searchPage(disc); code != http.StatusOK {
		t.Fatalf("repeated search answered %d, want 200", code)
	}
	for _, call := range h.transport.recorded()[asked:] {
		if call.host == "api.deezer.com" && call.path == "/search/album" && call.query == "probe album" {
			t.Errorf("a repeated search asked Deezer again although its answer was cached: %v", call)
		}
	}
}

func TestVocabulary_IsKeptInRedisWhenThereIsOne(t *testing.T) {
	h := newHarness(t, prodLikeProviders())
	h.deps.Redis = goredis.NewClient(&goredis.Options{Addr: miniredis.RunT(t).Addr()})

	if h.start().SpellingHints == nil {
		t.Error("no vocabulary store although redis is given")
	}
}

func TestSearch_WithRedisFillsMissingArtworkThroughTheChain(t *testing.T) {
	h := newHarness(t, everyProviderOff())
	h.deps.Redis = goredis.NewClient(&goredis.Options{Addr: miniredis.RunT(t).Addr()})
	disc := h.start()

	searchPage(disc)
	h.deps.Tracker.Wait()

	if !slices.Contains(h.transport.hosts(), "itunes.apple.com") {
		t.Errorf("a search with an artwork cache never filled artwork; asked %v", h.transport.hosts())
	}
}

func TestNew_LogsOneLinePerProviderForAProdLikeConfig(t *testing.T) {
	h := newHarness(t, prodLikeProviders())
	h.start()

	assertLines(t, h.logLines("discovery.provider"), []string{
		`level=INFO msg=discovery.provider name=deezer on=true jobs=search,artist,album,artwork,charts,details,featured,lyrics,similar`,
		`level=INFO msg=discovery.provider name=applemusic on=true jobs=search,artist,album`,
		`level=INFO msg=discovery.provider name=musicbrainz on=true jobs=artist,enrichment,featured,recordings`,
		`level=INFO msg=discovery.provider name=soundcloud on=true jobs=search,artist,album,related,artwork`,
		`level=INFO msg=discovery.provider name=youtube on=true jobs=search,artwork`,
		`level=INFO msg=discovery.provider name=spotify on=true jobs=search,artist,album,artwork`,
		`level=INFO msg=discovery.provider name=itunes on=true jobs=album,artwork`,
		`level=INFO msg=discovery.provider name=coverartarchive on=true jobs=artwork`,
		`level=INFO msg=discovery.provider name=discogs on=false reason="DISCOGS_TOKEN is empty"`,
		`level=INFO msg=discovery.provider name=fanarttv on=true jobs=artwork`,
		`level=INFO msg=discovery.provider name=genius on=true jobs=artwork,unreleased`,
		`level=INFO msg=discovery.provider name=theaudiodb on=true jobs=artwork`,
		`level=INFO msg=discovery.provider name=trackers on=true jobs=unreleased`,
	})
}

func TestNew_LogsWhichKeyEachKeyedProviderIsMissing(t *testing.T) {
	h := newHarness(t, everyProviderOff())
	h.start()

	assertLines(t, h.logLines("discovery.provider"), []string{
		`level=INFO msg=discovery.provider name=deezer on=true jobs=search,artist,album,artwork,charts,details,featured,lyrics,similar`,
		`level=INFO msg=discovery.provider name=applemusic on=true jobs=search,artist,album`,
		`level=INFO msg=discovery.provider name=musicbrainz on=false reason="MUSICBRAINZ_USER_AGENT is empty"`,
		`level=INFO msg=discovery.provider name=soundcloud on=true jobs=search,artist,album,related,artwork`,
		`level=INFO msg=discovery.provider name=youtube on=true jobs=search,artwork`,
		`level=INFO msg=discovery.provider name=spotify on=true jobs=search,artist,album,artwork`,
		`level=INFO msg=discovery.provider name=itunes on=true jobs=album,artwork`,
		`level=INFO msg=discovery.provider name=coverartarchive on=true jobs=artwork`,
		`level=INFO msg=discovery.provider name=discogs on=false reason="DISCOGS_TOKEN is empty"`,
		`level=INFO msg=discovery.provider name=fanarttv on=false reason="FANARTTV_API_KEY is empty"`,
		`level=INFO msg=discovery.provider name=genius on=false reason="GENIUS_ACCESS_TOKEN is empty"`,
		`level=INFO msg=discovery.provider name=theaudiodb on=false reason="THEAUDIODB_API_KEY is empty"`,
		`level=INFO msg=discovery.provider name=trackers on=false reason="GOOGLE_SHEETS_API_KEY is empty"`,
	})
}

func TestNew_WithEveryKeySetDiscogsServesArtwork(t *testing.T) {
	h := newHarness(t, everyProviderOn())
	h.start()

	if line := lineNaming(h.logLines("discovery.provider"), "discogs"); line != `level=INFO msg=discovery.provider name=discogs on=true jobs=artwork` {
		t.Errorf("discogs line = %q, want it on for artwork once DISCOGS_TOKEN is set", line)
	}
}

func TestNew_RefusesDepsWithoutTheHandlesItNeeds(t *testing.T) {
	_, err := search.New(search.Deps{})
	if err == nil {
		t.Fatal("New accepted deps with no transport, logger or lifecycle")
	}
	want := "discovery: deps are missing Lifecycle, Background, Tracker, Logger, Activity, ClientEvents, Transport, CacheOps, ProviderTimeout, ChartTimeout"
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err, want)
	}
}

func TestNew_NamesOnlyTheHandleThatIsMissing(t *testing.T) {
	h := newHarness(t, prodLikeProviders())
	h.deps.ChartTimeout = 0

	_, err := search.New(h.deps)
	if err == nil || err.Error() != "discovery: deps are missing ChartTimeout" {
		t.Errorf("error = %v, want only ChartTimeout named", err)
	}
}

func assertLines(t *testing.T, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("lines =\n  %s\nwant\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}

func lineNaming(lines []string, name string) string {
	for _, line := range lines {
		if strings.Contains(line, " name="+name+" ") {
			return line
		}
	}
	return ""
}

type activityRecorder struct {
	mu     sync.Mutex
	events []string
}

func (r *activityRecorder) EmitActivity(eventType string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, eventType)
}

func (r *activityRecorder) emitted() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.events)
}

func (h *harness) searchOptions() []string {
	h.t.Helper()
	lines := h.logLines("discovery.search")
	if len(lines) != 1 {
		h.t.Fatalf("discovery.search lines = %v, want exactly one", lines)
	}
	return strings.Split(logField(lines[0], "options"), ",")
}

func TestNew_LogsTheSearchOptionsForAProdLikeConfig(t *testing.T) {
	h := newHarness(t, prodLikeProviders())
	h.start()

	assertLines(t, h.logLines("discovery.search"), []string{
		`level=INFO msg=discovery.search options=history,deadline,artwork,find_related,event_store,activity`,
	})
}

func TestSearchSettings_EachSwitchAddsItsOptionOnlyWhenOn(t *testing.T) {
	tests := []struct {
		setting string
		turnOn  func(h *harness)
		option  string
	}{
		{"TailDemotion", func(h *harness) { h.deps.Search.TailDemotion = true }, "tail_demotion"},
		{"CrossKindProminence", func(h *harness) { h.deps.Search.CrossKindProminence = true }, "cross_kind_prominence"},
		{"Exploration", func(h *harness) { h.deps.Search.Exploration, h.deps.Search.ExplorationRate = true, 0.1 }, "exploration"},
		{"BehavioralRanking", func(h *harness) { h.deps.Search.BehavioralRanking = true }, "behavioral_ranking"},
		{"Pool", func(h *harness) { h.deps.Pool = unreachablePool(h.t) }, "favorites"},
		{"Redis", func(h *harness) {
			h.deps.Redis = goredis.NewClient(&goredis.Options{Addr: miniredis.RunT(h.t).Addr()})
		}, "provider_result_cache"},
	}
	for _, tt := range tests {
		t.Run(tt.setting, func(t *testing.T) {
			off := newHarness(t, prodLikeProviders())
			off.start()
			if slices.Contains(off.searchOptions(), tt.option) {
				t.Errorf("%s off, yet search was given %q", tt.setting, tt.option)
			}
			on := newHarness(t, prodLikeProviders())
			tt.turnOn(on)
			on.start()
			if !slices.Contains(on.searchOptions(), tt.option) {
				t.Errorf("%s on, yet search was not given %q; options %v", tt.setting, tt.option, on.searchOptions())
			}
		})
	}
}

func twoResults() []search.SearchResult {
	return []search.SearchResult{{Title: "First"}, {Title: "Second"}}
}

func TestSearchSettings_ExplorationUsesTheConfiguredRate(t *testing.T) {
	tests := []struct {
		name        string
		exploration bool
		rate        float64
		want        bool
	}{
		{"on at rate 1 always explores", true, 1, true},
		{"on at rate 0 never explores", true, 0, false},
		{"off ignores the rate", false, 1, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, everyProviderOff())
			h.deps.Search.Exploration = tt.exploration
			h.deps.Search.ExplorationRate = tt.rate

			if _, explored := h.start().Search.MaybeExplore(twoResults()); explored != tt.want {
				t.Errorf("explored = %v, want %v", explored, tt.want)
			}
		})
	}
}

func TestSearchSettings_BehavioralRankingReadsTheEventStoreOnlyWhenOn(t *testing.T) {
	for _, on := range []bool{false, true} {
		h := newHarness(t, everyProviderOff())
		h.deps.Pool = unreachablePool(t)
		h.deps.Search.BehavioralRanking = on

		err := h.start().Search.RefreshBehavioralScores(context.Background())
		if on && err == nil {
			t.Error("behavioral ranking on, yet refreshing never asked the unreachable event store")
		}
		if !on && err != nil {
			t.Errorf("behavioral ranking off, yet refreshing asked the event store: %v", err)
		}
	}
}

func providerCacheTTLs(t *testing.T, providers search.ProviderSettings, target string) []time.Duration {
	t.Helper()
	h := newHarness(t, providers)
	redis := miniredis.RunT(t)
	h.deps.Redis = goredis.NewClient(&goredis.Options{Addr: redis.Addr()})
	h.deps.Search.ProviderCacheTTL = 7 * time.Minute
	disc := h.start()
	serve(disc, target)
	h.deps.Tracker.Wait()
	var ttls []time.Duration
	for _, key := range redis.Keys() {
		if strings.HasPrefix(key, "discovery:provider:") {
			ttls = append(ttls, redis.TTL(key))
		}
	}
	return ttls
}

func TestSearchSettings_ProviderResultsAreCachedForTheConfiguredTTL(t *testing.T) {
	ttls := providerCacheTTLs(t, everyProviderOn(), "/search?q=probe+album&kinds=album&save_history=false")
	if len(ttls) == 0 || slices.ContainsFunc(ttls, func(ttl time.Duration) bool { return ttl != 7*time.Minute }) {
		t.Errorf("cached provider answers live %v, want the 7m provider TTL for every provider", ttls)
	}
}

func TestSearch_NeverAsksMusicBrainzEvenWithItsUserAgentSet(t *testing.T) {
	h := newHarness(t, everyProviderOn())
	disc := h.start()
	query := &search.SearchQuery{Raw: "probe song", Kinds: search.AllKinds()}

	_, statuses := disc.Search.InspectSearchWithStatuses(context.Background(), query)
	h.deps.Tracker.Wait()
	for _, status := range statuses {
		if status.Provider == search.ProviderMusicBrainz {
			t.Errorf("search reported MusicBrainz as a searcher: %+v", status)
		}
	}
}

func TestSearch_ActivityHearsOfEachStoredSearch(t *testing.T) {
	feed := &activityRecorder{}
	h := newHarness(t, everyProviderOff())
	h.deps.Pool = testdb.Pool(t)
	h.deps.Activity = feed
	disc := h.start()

	serve(disc, "/search?q=probe+album&kinds=album&save_history=false")
	h.deps.Tracker.Wait()

	if !slices.Contains(feed.emitted(), "search_performed") {
		t.Errorf("activity heard %v, want a search_performed", feed.emitted())
	}
}

var hostOwners = map[string]string{
	"api.deezer.com":          "deezer",
	"music.apple.com":         "applemusic",
	"amp-api.music.apple.com": "applemusic",
	"musicbrainz.org":         "musicbrainz",
	"soundcloud.com":          "soundcloud",
	"api-v2.soundcloud.com":   "soundcloud",
	"music.youtube.com":       "youtube",
	"open.spotify.com":        "spotify",
	"api.spotify.com":         "spotify",
	"itunes.apple.com":        "itunes",
	"coverartarchive.org":     "coverartarchive",
	"api.discogs.com":         "discogs",
	"webservice.fanart.tv":    "fanarttv",
	"api.genius.com":          "genius",
	"theaudiodb.com":          "theaudiodb",
	"www.theaudiodb.com":      "theaudiodb",
	"sheets.googleapis.com":   "trackers",
}

func (rt *recordingTransport) owners() []string {
	var owners []string
	for _, host := range rt.hosts() {
		if owner, ok := hostOwners[host]; ok && !slices.Contains(owners, owner) {
			owners = append(owners, owner)
		}
	}
	return owners
}

func servedBy(t *testing.T, targets ...string) []string {
	t.Helper()
	h := newHarness(t, everyProviderOn())
	disc := h.start()
	for _, target := range targets {
		serve(disc, target)
	}
	h.deps.Tracker.Wait()
	return h.transport.owners()
}

var jobProbes = map[string]func(t *testing.T, provider string) []string{
	"search": func(t *testing.T, _ string) []string {
		h := newHarness(t, everyProviderOn())
		return searchOrder(t, h, h.start())
	},
	"artist": func(t *testing.T, provider string) []string {
		if provider == "musicbrainz" {
			return anchoredArtistContent(t)
		}
		return servedBy(t, "/artists/"+provider+"/1/content")
	},
	"album": func(t *testing.T, provider string) []string {
		return servedBy(t, "/albums/"+provider+"/1/tracks")
	},
	"related": func(t *testing.T, provider string) []string {
		return servedBy(t, "/tracks/"+provider+"/1/related")
	},
	"artwork": func(t *testing.T, _ string) []string {
		h := newHarness(t, everyProviderOn())
		h.transport.artistHasLinks = true
		disc := h.start()
		serve(disc, "/enrichment?kind=artist&title=Probe+Artist")
		serve(disc, "/enrichment?kind=album&title=Probe+Album&subtitle=Probe+Artist")
		return h.transport.owners()
	},
	"charts": func(t *testing.T, _ string) []string {
		var names []string
		for _, chart := range newHarness(t, everyProviderOn()).start().Charts {
			names = append(names, chart.Name().String())
		}
		return names
	},
	"details": func(t *testing.T, provider string) []string {
		return servedBy(t, "/enrichment/"+provider+"?kind=track&title=Probe+Song&subtitle=Probe+Artist")
	},
	"enrichment": func(t *testing.T, _ string) []string {
		return servedBy(t, "/enrichment?kind=artist&title=Probe+Artist")
	},
	"featured": func(t *testing.T, _ string) []string {
		h := newHarness(t, everyProviderOn())
		_, _ = h.start().FeaturedArtists.Resolve(context.Background(), "Probe Artist", "Probe Song")
		return h.transport.owners()
	},
	"lyrics": func(t *testing.T, _ string) []string {
		return servedBy(t, "/lyrics?title=Probe+Song&subtitle=Probe+Artist")
	},
	"similar": func(t *testing.T, _ string) []string {
		h := newHarness(t, everyProviderOff())
		disc := h.start()
		serve(disc, "/search?q="+strings.ReplaceAll(similarArtistQuery, " ", "+")+"&kinds=artist&save_history=false")
		h.deps.Tracker.Wait()
		for _, call := range h.transport.recorded() {
			if call.host == "api.deezer.com" && strings.HasSuffix(call.path, "/albums") {
				return []string{"deezer"}
			}
		}
		return nil
	},
	"unreleased": func(t *testing.T, _ string) []string {
		return servedBy(t, "/artists/deezer/1/unreleased?name=Probe+Artist")
	},
	"recordings": func(t *testing.T, _ string) []string {
		h := newHarness(t, everyProviderOn())
		_, _ = h.start().Recordings.RecordingsByISRC(context.Background(), "USRC17607839")
		return h.transport.owners()
	},
}

func loggedJobs(t *testing.T) map[string][]string {
	t.Helper()
	h := newHarness(t, everyProviderOn())
	h.start()
	jobs := map[string][]string{}
	for _, line := range h.logLines("discovery.provider") {
		name := logField(line, "name")
		if list := logField(line, "jobs"); list != "" {
			jobs[name] = strings.Split(list, ",")
		}
	}
	return jobs
}

func logField(line, key string) string {
	for _, field := range strings.Fields(line) {
		if value, ok := strings.CutPrefix(field, key+"="); ok {
			return value
		}
	}
	return ""
}

func TestEveryJobAProviderLogsIsWiredToThatProvider(t *testing.T) {
	logged := loggedJobs(t)
	if len(logged) != 13 {
		t.Fatalf("logged jobs for %d providers, want all 13 on: %v", len(logged), logged)
	}
	for provider, jobs := range logged {
		for _, job := range jobs {
			t.Run(provider+"/"+job, func(t *testing.T) {
				probe, ok := jobProbes[job]
				if !ok {
					t.Fatalf("%s logs job %q, which no probe exercises", provider, job)
				}
				if reached := probe(t, provider); !slices.Contains(reached, provider) {
					t.Errorf("%s logs %q, but doing that job reached %v", provider, job, reached)
				}
			})
		}
	}
}

func anchoredArtistContent(t *testing.T) []string {
	t.Helper()
	h := storedIdentityHarness(t, everyProviderOn())
	serve(h.start(), "/artists/deezer/1/content")
	if h.transport.askedReleaseGroups() {
		return []string{"musicbrainz"}
	}
	return nil
}

func storedIdentityHarness(t *testing.T, providers search.ProviderSettings) *harness {
	t.Helper()
	pool := testdb.Pool(t)
	ctx := context.Background()
	forget := func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM entity_identity WHERE provider = 'deezer' AND external_id = '1' AND kind = 'artist'`)
	}
	forget()
	if _, err := pool.Exec(ctx, `INSERT INTO entity_identity (provider, external_id, kind, mbid, xref) VALUES ('deezer', '1', 'artist', $1, '{"deezer":"1"}')`, probeArtistMBID); err != nil {
		t.Fatalf("seed identity: %v", err)
	}
	t.Cleanup(forget)
	h := newHarness(t, providers)
	h.deps.Pool = pool
	return h
}

func (rt *recordingTransport) askedReleaseGroups() bool {
	for _, call := range rt.recorded() {
		if call.host == "musicbrainz.org" && strings.HasPrefix(call.path, "/ws/2/release-group") {
			return true
		}
	}
	return false
}

func TestArtistContent_WithoutMusicBrainzAStoredIdentityIsNotAnchored(t *testing.T) {
	h := storedIdentityHarness(t, everyProviderOff())

	if code := serve(h.start(), "/artists/deezer/1/content"); code >= 500 {
		t.Errorf("artist content answered %d without MusicBrainz", code)
	}
	if h.transport.askedReleaseGroups() {
		t.Error("artist content asked MusicBrainz for release groups although it is off")
	}
}

const (
	probeArtistMBID    = "11111111-1111-1111-1111-111111111111"
	probeAlbumMBID     = "22222222-2222-2222-2222-222222222222"
	similarArtistQuery = "probe similar"
	artistLinks        = `{"type":"free streaming","url":{"resource":"https://www.deezer.com/artist/2"}},` +
		`{"type":"free streaming","url":{"resource":"https://open.spotify.com/artist/4"}},` +
		`{"type":"discogs","url":{"resource":"https://www.discogs.com/artist/3"}}`
)

type providerCall struct {
	method  string
	host    string
	path    string
	query   string
	timeout time.Duration
}

type recordingTransport struct {
	mu             sync.Mutex
	calls          []providerCall
	artistHasLinks bool
}

func (rt *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.record(req)
	status, body := http.StatusNotFound, `{}`
	if req.URL.Host == "musicbrainz.org" && (strings.HasPrefix(req.URL.Path, "/ws/2/artist/") || strings.HasPrefix(req.URL.Path, "/ws/2/release-group/")) {
		status = http.StatusOK
	}
	if req.URL.Host == "musicbrainz.org" && req.URL.Path == "/ws/2/artist/" {
		body = `{"artists":[{"id":"` + probeArtistMBID + `","name":"Probe Artist","score":100}]}`
	}
	if rt.artistHasLinks && req.URL.Path == "/ws/2/artist/"+probeArtistMBID {
		body = `{"id":"` + probeArtistMBID + `","name":"Probe Artist","relations":[` + artistLinks + `]}`
	}
	if req.URL.Host == "musicbrainz.org" && req.URL.Path == "/ws/2/release-group/" {
		status, body = http.StatusOK, `{"release-groups":[{"id":"`+probeAlbumMBID+`","title":"Probe Album","score":100,"artist-credit":[{"name":"Probe Artist","artist":{"id":"`+probeArtistMBID+`","name":"Probe Artist"}}]}]}`
	}
	if req.URL.Host == "api.deezer.com" && req.URL.Path == "/search/artist" && req.URL.Query().Get("q") == similarArtistQuery {
		status, body = http.StatusOK, `{"data":[{"id":2,"name":"Probe Similar","link":"https://www.deezer.com/artist/2"}]}`
	}
	if req.URL.Host == "api.deezer.com" && req.URL.Path == "/search/album" {
		status, body = http.StatusOK, `{"data":[{"id":1,"title":"Probe Album","artist":{"id":2,"name":"Probe Artist"}}]}`
	}
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}, nil
}

func (rt *recordingTransport) record(req *http.Request) {
	var timeout time.Duration
	if deadline, ok := req.Context().Deadline(); ok {
		timeout = time.Duration(math.Round(time.Until(deadline).Seconds())) * time.Second
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.calls = append(rt.calls, providerCall{method: req.Method, host: req.URL.Host, path: req.URL.Path, query: req.URL.Query().Get("q"), timeout: timeout})
}

func (rt *recordingTransport) recorded() []providerCall {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return slices.Clone(rt.calls)
}

func (rt *recordingTransport) hosts() []string {
	var hosts []string
	for _, call := range rt.recorded() {
		hosts = append(hosts, call.host)
	}
	return hosts
}

func (rt *recordingTransport) timeoutsByHost() map[string]time.Duration {
	timeouts := map[string]time.Duration{}
	for _, call := range rt.recorded() {
		timeouts[call.host] = call.timeout
	}
	return timeouts
}

func prodLikeProviders() search.ProviderSettings {
	return search.ProviderSettings{
		MusicBrainzUserAgent: "altune-test/1.0",
		FanartTVAPIKey:       "fanart-key",
		GeniusAccessToken:    "genius-token",
		TheAudioDBAPIKey:     "theaudiodb-key",
		GoogleSheetsAPIKey:   "sheets-key",
		TrackerHubSheetID:    "hub",
	}
}

func everyProviderOn() search.ProviderSettings {
	providers := prodLikeProviders()
	providers.DiscogsToken = "discogs-token"
	return providers
}

func everyProviderOff() search.ProviderSettings {
	return search.ProviderSettings{TrackerHubSheetID: "hub"}
}

type harness struct {
	t         *testing.T
	transport *recordingTransport
	logs      *bytes.Buffer
	deps      search.Deps
}

func newHarness(t *testing.T, providers search.ProviderSettings) *harness {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	jobs := &background.Tracker{}
	t.Cleanup(func() { cancel(); jobs.Wait() })
	logs := &bytes.Buffer{}
	transport := &recordingTransport{}
	return &harness{t: t, transport: transport, logs: logs, deps: search.Deps{
		Lifecycle:       ctx,
		Background:      jobs,
		Tracker:         searchtest.NewTracker(t),
		Logger:          slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{ReplaceAttr: dropTime})),
		Transport:       transport,
		Activity:        search.NoActivityFeed{},
		ClientEvents:    search.NoClientEvents{},
		ProviderTimeout: 10 * time.Second,
		ChartTimeout:    15 * time.Second,
		CacheOps:        new(expvar.Map),
		Providers:       providers,
		Search:          search.SearchSettings{Deadline: 1500 * time.Millisecond},
		Unreleased:      search.UnreleasedSettings{Tuning: search.UnreleasedConfig{MaxPages: 1, MaxDetails: 1, DetailConcurrency: 1, Wait: time.Second, JobTimeout: 5 * time.Second}},
	}}
}

func dropTime(_ []string, attr slog.Attr) slog.Attr {
	if attr.Key == slog.TimeKey {
		return slog.Attr{}
	}
	return attr
}

func (h *harness) start() *search.Discovery {
	h.t.Helper()
	disc, err := search.New(h.deps)
	if err != nil {
		h.t.Fatalf("New: %v", err)
	}
	return disc
}

func (h *harness) logLines(message string) []string {
	var lines []string
	for _, line := range strings.Split(h.logs.String(), "\n") {
		if strings.Contains(line, "msg="+message+" ") || strings.HasSuffix(line, "msg="+message) {
			lines = append(lines, line)
		}
	}
	return lines
}

func serve(disc *search.Discovery, target string) int {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req = req.WithContext(requestuser.ContextWithUserID(req.Context(), ids.NewUserId(uuid.New())))
	rec := httptest.NewRecorder()
	disc.Handler.Routes().ServeHTTP(rec, req)
	return rec.Code
}

func (h *harness) optionsLoggedAs(message string) []string {
	h.t.Helper()
	lines := h.logLines(message)
	if len(lines) != 1 {
		h.t.Fatalf("%s lines = %v, want exactly one", message, lines)
	}
	return strings.Split(logField(lines[0], "options"), ",")
}

func everySearchSettingOn(h *harness) {
	h.deps.Pool = unreachablePool(h.t)
	h.deps.Redis = goredis.NewClient(&goredis.Options{Addr: miniredis.RunT(h.t).Addr()})
	h.deps.Activity = &activityRecorder{}
	h.deps.Search.TailDemotion = true
	h.deps.Search.CrossKindProminence = true
	h.deps.Search.Exploration = true
	h.deps.Search.BehavioralRanking = true
}

func TestRankingOnlySearch_RanksWithoutDeadlineArtworkResultCachesOrRecording(t *testing.T) {
	h := newHarness(t, prodLikeProviders())
	everySearchSettingOn(h)
	h.start().RankingOnlySearch()

	assertLines(t, h.optionsLoggedAs("discovery.ranking_only_search"), []string{
		"history", "tail_demotion", "cross_kind_prominence", "vocabulary",
	})
}

func TestRankingOnlySearch_WithoutRedisKeepsOnlyTheHistory(t *testing.T) {
	h := newHarness(t, prodLikeProviders())
	h.start().RankingOnlySearch()

	assertLines(t, h.optionsLoggedAs("discovery.ranking_only_search"), []string{"history"})
}

func TestRankingOnlySearch_AsksTheSameSearchersAsTheServingSearch(t *testing.T) {
	h := newHarness(t, everyProviderOn())
	disc := h.start()
	query := &search.SearchQuery{Raw: "probe song", Kinds: map[search.ResultKind]bool{search.ResultKindAlbum: true}}

	_, statuses := disc.RankingOnlySearch().InspectSearchWithStatuses(context.Background(), query)
	var asked []string
	for _, status := range statuses {
		asked = append(asked, status.Provider.String())
	}
	assertLines(t, asked, searchOrder(t, h, disc))
}

func TestIdentityStore_AGivenStoreServesSearchWithoutAPool(t *testing.T) {
	h := newHarness(t, prodLikeProviders())
	h.deps.IdentityStore = &search.FakeIdentityStore{}
	h.start()

	options := h.searchOptions()
	if !slices.Contains(options, "identity_store") || slices.Contains(options, "favorites") {
		t.Errorf("options = %v, want the given identity store and no favorites without a pool", options)
	}
}

func sourced(kind search.ResultKind, title, credit string, provider search.ProviderName, id string) search.SearchResult {
	return search.SearchResult{
		Kind: kind, Title: title, Subtitle: credit,
		Sources: []search.ProviderLink{{Provider: provider, ExternalID: id}},
	}
}

func merged(groups ...[]search.SearchResult) []search.SearchResult {
	var out []search.SearchResult
	for _, entity := range search.RankMerge(groups) {
		out = append(out, entity.Result)
	}
	return out
}

func TestMerge_AlbumsWithoutAUPCNeverMergeAtTheUPCTier(t *testing.T) {
	queen := sourced(search.ResultKindAlbum, "Greatest Hits", "Queen", search.ProviderDeezer, "1")
	abba := sourced(search.ResultKindAlbum, "Greatest Hits", "ABBA", search.ProviderSpotify, "2")

	if got := merged([]search.SearchResult{queen}, []search.SearchResult{abba}); len(got) != 2 {
		t.Fatalf("got %d albums, want Queen's and ABBA's kept apart when neither has a UPC", len(got))
	}
}

func TestMerge_ASecondSameNameArtistFromOneProviderJoinsNoOtherCard(t *testing.T) {
	famous := sourced(search.ResultKindArtist, "Che", "", search.ProviderDeezer, "1")
	famous.FanCount = 100
	lesser := sourced(search.ResultKindArtist, "Che", "", search.ProviderDeezer, "2")
	lesser.FanCount = 50
	spotify := sourced(search.ResultKindArtist, "Che", "", search.ProviderSpotify, "s1")

	got := merged([]search.SearchResult{famous, lesser}, []search.SearchResult{spotify})

	if len(got) != 2 || len(got[0].Sources) != 2 || len(got[1].Sources) != 1 {
		t.Fatalf("got %+v, want the famous Che joined by Spotify and the lesser Che alone", got)
	}
	if got[1].Sources[0].ExternalID != "2" {
		t.Errorf("lone card = %+v, want Deezer's lesser Che", got[1].Sources)
	}
}

func TestMerge_OfTwoSameNameArtistsWithEqualFansTheFirstListedWins(t *testing.T) {
	first := sourced(search.ResultKindArtist, "Che", "", search.ProviderDeezer, "1")
	second := sourced(search.ResultKindArtist, "Che", "", search.ProviderDeezer, "2")
	first.FanCount, second.FanCount = 50, 50
	spotify := sourced(search.ResultKindArtist, "Che", "", search.ProviderSpotify, "s1")

	got := merged([]search.SearchResult{first, second}, []search.SearchResult{spotify})

	if len(got) != 2 || len(got[0].Sources) != 2 || len(got[1].Sources) != 1 {
		t.Fatalf("got %+v, want the first Che joined by Spotify and the second Che alone", got)
	}
	if got[0].Sources[0].ExternalID != "1" || got[1].Sources[0].ExternalID != "2" {
		t.Errorf("joined card = %+v, lone card = %+v, want Spotify on the first Che and the second Che alone", got[0].Sources, got[1].Sources)
	}
}

func TestMerge_ATitleWithNoLettersCannotProveTwoTakesAreOneVersion(t *testing.T) {
	complete := sourced(search.ResultKindTrack, "÷", "Ed Sheeran", search.ProviderDeezer, "1")
	complete.Album, complete.ImageURL, complete.Duration = "÷", "https://x/art.jpg", 200
	coded := sourced(search.ResultKindTrack, "÷", "Ed Sheeran", search.ProviderSpotify, "2")
	coded.ISRC, coded.Duration = "GBAHS1700001", 200

	got := merged([]search.SearchResult{complete}, []search.SearchResult{coded})

	if len(got) != 1 {
		t.Fatalf("got %d results, want the two takes merged into one track", len(got))
	}
	if got[0].ISRC != "" {
		t.Errorf("ISRC = %q, want none: the merged track cannot prove the ISRC is its own", got[0].ISRC)
	}
}

func TestCollapseArtistDuplicates_FoldsCardsWhoseNamesDifferOnlyInCaseAndAccents(t *testing.T) {
	accented := sourced(search.ResultKindArtist, "Beyoncé", "", search.ProviderDeezer, "1")
	plain := sourced(search.ResultKindArtist, "BEYONCE", "", search.ProviderSpotify, "2")

	if got := search.RankCollapseArtistDuplicates([]search.SearchResult{accented, plain}); len(got) != 1 {
		t.Fatalf("got %d artist cards, want one", len(got))
	}
}
