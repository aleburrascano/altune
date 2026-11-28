package download_test

import (
	"altune/go-api/internal/download"
	"altune/go-api/internal/download/downloadtest"
	"altune/go-api/internal/download/internal/adapters/memory"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/platform/outbound"
	"altune/go-api/internal/platform/outbound/outboundtest"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/ids"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"
)

func fixtureTiers() download.SourceOrder {
	return download.SourceOrder{
		First:    []download.AudioSource{downloadtest.NewArtTrackSource(downloadtest.Clip)},
		Fallback: []download.AudioSource{downloadtest.NewUploadSource(downloadtest.Clip)},
	}
}

type orphanSink struct{}

func (orphanSink) RecordOrphanedAudio(context.Context, ports.OrphanedAudio) error { return nil }

func newClient(timeout time.Duration, transport http.RoundTripper) *http.Client {
	return &http.Client{Timeout: timeout, Transport: transport}
}

func offlineDeps(cfg *config.Config, sources download.SourceOrder) download.Deps {
	cfg.AcquisitionDeezerPreviewTimeout = time.Second
	return download.Deps{
		Config:            cfg,
		Transport:         http.DefaultTransport,
		Events:            events.NoopPublisher(),
		NewClient:         newClient,
		Library:           memory.NewLibrary(),
		AudioStore:        memory.NewAudioStore(),
		StoredAudio:       ports.NoopStoredAudioReader(),
		Orphans:           orphanSink{},
		RecordingResolver: download.NoopRecordingResolver(),
		RecordingLookup:   download.NoopRecordingLookup(),
		Rerecordings:      download.NoopRerecordingFinder(),
		Sources:           sources,
	}
}

func newOfflineModule(t *testing.T, cfg *config.Config, sources download.SourceOrder) *download.Module {
	t.Helper()
	module, err := download.New(t.Context(), offlineDeps(cfg, sources))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !module.On() {
		t.Fatal("New with sources and an audio store left acquisition off")
	}
	t.Cleanup(func() { module.Scheduler().Shutdown(context.Background()) })
	return module
}

func TestNew_RefusesAMissingTransport(t *testing.T) {
	deps := offlineDeps(&config.Config{Env: "test", AcquisitionDownloadConcurrency: 1}, fixtureTiers())
	deps.Transport = nil

	module, err := download.New(t.Context(), deps)

	if err == nil || module != nil || !strings.Contains(err.Error(), "Transport") {
		t.Fatalf("New = %v, %v, want an error naming the missing transport and no module", module, err)
	}
}

func TestNew_RefusesAMissingCollaboratorInsteadOfPanickingLater(t *testing.T) {
	cases := map[string]func(*download.Deps){
		"config":      func(d *download.Deps) { d.Config = nil },
		"events":      func(d *download.Deps) { d.Events = nil },
		"client":      func(d *download.Deps) { d.NewClient = nil },
		"library":     func(d *download.Deps) { d.Library = nil },
		"orphans":     func(d *download.Deps) { d.Orphans = nil },
		"stored":      func(d *download.Deps) { d.StoredAudio = nil },
		"resolver":    func(d *download.Deps) { d.RecordingResolver = nil },
		"lookup":      func(d *download.Deps) { d.RecordingLookup = nil },
		"rerecording": func(d *download.Deps) { d.Rerecordings = nil },
	}
	for name, drop := range cases {
		t.Run(name, func(t *testing.T) {
			deps := offlineDeps(&config.Config{Env: "test", AcquisitionDownloadConcurrency: 1}, fixtureTiers())
			drop(&deps)

			if module, err := download.New(t.Context(), deps); err == nil || module != nil {
				t.Errorf("New = %v, %v, want a refusal", module, err)
			}
		})
	}
}

func TestNew_WithoutAnAudioStoreBuildsAModuleWithAcquisitionOff(t *testing.T) {
	deps := offlineDeps(&config.Config{Env: "test", AcquisitionDownloadConcurrency: 1}, fixtureTiers())
	deps.AudioStore = nil

	module, err := download.New(t.Context(), deps)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if module.On() || module.Scheduler() != nil {
		t.Errorf("On = %v, Scheduler = %v, want acquisition off without an audio store", module.On(), module.Scheduler())
	}
	if plan, _ := module.Upgrade(); plan != nil {
		t.Error("Upgrade plan exists with acquisition off, want none")
	}
	router := chi.NewRouter()
	module.Routes().Bounded(router)
	module.Routes().LongLived(router)
	if len(router.Routes()) != 0 {
		t.Errorf("routes = %v, want none with acquisition off", router.Routes())
	}
}

func TestNew_OneUserMayQueuePastTheOldGlobalDepth(t *testing.T) {
	const concurrency = 2
	s := newOfflineModule(t, &config.Config{AcquisitionDownloadConcurrency: 1, AcquisitionConcurrency: concurrency}, fixtureTiers()).Scheduler()

	userA := ids.NewUserId(uuid.New())
	const pastOldGlobalDepth = concurrency*4 + 5
	for i := 0; i < pastOldGlobalDepth; i++ {
		if err := s.Schedule(context.Background(), userA, ids.NewTrackId(), ""); err != nil {
			t.Fatalf("schedule %d of %d for one user: err = %v, want nil (the queue has no depth limit)", i+1, pastOldGlobalDepth, err)
		}
	}
}

func TestNew_DoesNotCapAPrincipalsQueue(t *testing.T) {
	const concurrency = 2
	const principalCap = concurrency
	s := newOfflineModule(t, &config.Config{AcquisitionDownloadConcurrency: 1, AcquisitionConcurrency: concurrency}, fixtureTiers()).Scheduler()

	userA := ids.NewUserId(uuid.New())
	for i := 0; i < principalCap+2; i++ {
		if err := s.Schedule(context.Background(), userA, ids.NewTrackId(), ""); err != nil {
			t.Fatalf("schedule %d past userA's old share: err = %v, want nil (the cap no longer refuses)", i+1, err)
		}
	}
	if err := s.Schedule(context.Background(), ids.NewUserId(uuid.New()), ids.NewTrackId(), ""); err != nil {
		t.Fatalf("schedule for second principal: err = %v, want nil", err)
	}
}

func TestNew_AcquisitionPausedStartsPaused(t *testing.T) {
	s := newOfflineModule(t, &config.Config{AcquisitionDownloadConcurrency: 1, AcquisitionPaused: true}, fixtureTiers()).Scheduler()

	if !s.Status(t.Context()).Paused {
		t.Fatal("Status().Paused = false straight out of New, want true so no worker claims before the startup switches apply")
	}
}

func TestNew_ReportsFingerprintingReadyOnlyWithAnAcoustIDKey(t *testing.T) {
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "fpcalc"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fpcalc stub: %v", err)
	}
	tests := []struct {
		name string
		key  string
		want bool
	}{
		{name: "no key", key: "", want: false},
		{name: "a key", key: "acoustid-test-key", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newOfflineModule(t, &config.Config{AcquisitionDownloadConcurrency: 1, FFmpegLocation: binDir, AcoustIDAPIKey: tt.key}, fixtureTiers()).Scheduler()

			if got := s.Status(t.Context()).Verification.Fpcalc; got != tt.want {
				t.Errorf("Status().Verification.Fpcalc = %v with fpcalc installed and AcoustID key %q, want %v", got, tt.key, tt.want)
			}
		})
	}
}

func liveTiers(t *testing.T, cfg *config.Config) (download.SourceOrder, download.Verification) {
	t.Helper()
	cfg.AcquisitionFileHostPageTimeout = time.Second
	tiers, tools, err := download.LiveTiers(t.Context(), cfg, http.DefaultTransport)
	if err != nil {
		t.Fatalf("LiveTiers: %v", err)
	}
	return tiers, tools
}

func TestLiveTiers_EachSourceFollowsItsSwitch(t *testing.T) {
	tests := []struct {
		name        string
		ytMusic     bool
		ytDlp       bool
		wantYtMusic bool
		wantYtDlp   bool
	}{
		{name: "both enabled (preserves current behavior)", ytMusic: true, ytDlp: true, wantYtMusic: true, wantYtDlp: true},
		{name: "ytmusic disabled", ytMusic: false, ytDlp: true, wantYtMusic: false, wantYtDlp: true},
		{name: "ytdlp disabled", ytMusic: true, ytDlp: false, wantYtMusic: true, wantYtDlp: false},
		{name: "both disabled", ytMusic: false, ytDlp: false, wantYtMusic: false, wantYtDlp: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sources, _ := liveTiers(t, &config.Config{YtMusicEnabled: tt.ytMusic, YtDLPEnabled: tt.ytDlp})
			names := sourceNames(sources.All())

			if got := containsSource(names, download.YtMusicSourceName); got != tt.wantYtMusic {
				t.Errorf("ytmusic present = %v, want %v (sources=%v)", got, tt.wantYtMusic, names)
			}
			if got := containsSource(names, download.YtDlpSourceName); got != tt.wantYtDlp {
				t.Errorf("ytdlp present = %v, want %v (sources=%v)", got, tt.wantYtDlp, names)
			}
		})
	}
}

func TestLiveTiers_WithEverySourceSwitchedOffTheCatalogLinkIsNotAddedAlone(t *testing.T) {
	tiers, _ := liveTiers(t, &config.Config{})

	if names := sourceNames(tiers.All()); len(names) != 0 {
		t.Errorf("sources = %v with YTMUSIC_ENABLED and YTDLP_ENABLED off, want none", names)
	}
}

func TestLiveTiers_YouTubeMusicSearchRunsFirstAndYtDlpIsTheFallback(t *testing.T) {
	tiers, _ := liveTiers(t, &config.Config{YtMusicEnabled: true, YtDLPEnabled: true})

	first, fallback := sourceNames(tiers.First), sourceNames(tiers.Fallback)
	if !containsSource(first, download.YtMusicSourceName) || !containsSource(first, download.YouTubeMusicSearchSourceName) {
		t.Errorf("first tier = %v, want the catalog id and the songs search", first)
	}
	if len(fallback) != 1 || fallback[0] != download.YtDlpSourceName {
		t.Errorf("fallback = %v, want only ytdlp", fallback)
	}
}

func TestLiveTiers_TheSongsSearchOutranksTheCatalogLinkForTheSameVideo(t *testing.T) {
	tiers, _ := liveTiers(t, &config.Config{YtMusicEnabled: true, YtDLPEnabled: true})
	first := sourceNames(tiers.First)

	if len(first) != 3 || first[0] != download.SavedLinkSourceName || first[1] != download.YouTubeMusicSearchSourceName || first[2] != download.YtMusicSourceName {
		t.Errorf("first tier = %v, want [%s %s %s]: duplicates keep the earlier source, and only the songs search carries the video's own length",
			first, download.SavedLinkSourceName, download.YouTubeMusicSearchSourceName, download.YtMusicSourceName)
	}
}

func TestLiveTiers_ReportWhetherYtDlpIsInstalled(t *testing.T) {
	for _, tt := range []struct {
		name      string
		installed bool
	}{
		{name: "no binary on PATH", installed: false},
		{name: "a runnable yt-dlp on PATH", installed: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			bin := t.TempDir()
			if tt.installed {
				if err := os.WriteFile(filepath.Join(bin, "yt-dlp"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
					t.Fatalf("write the stub yt-dlp: %v", err)
				}
			}
			t.Setenv("PATH", bin)

			tiers, tools := liveTiers(t, &config.Config{YtDLPEnabled: true})

			if tools.YtDlp != tt.installed {
				t.Errorf("tools report yt-dlp installed = %v, want %v", tools.YtDlp, tt.installed)
			}
			if names := sourceNames(tiers.All()); !containsSource(names, download.YtDlpSourceName) {
				t.Errorf("sources %v, want the yt-dlp source wired whether or not the binary is found", names)
			}
		})
	}
}

func TestYtDlpSearcher_PassesTheConfiguredRemoteComponentsToYtDlp(t *testing.T) {
	bin := t.TempDir()
	argv := filepath.Join(t.TempDir(), "argv.txt")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + argv + "\nexit 1\n"
	if err := os.WriteFile(filepath.Join(bin, "yt-dlp"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	_, _ = download.YtDlpSearcher(&config.Config{YtDLPJSRuntime: "node", YtDLPRemoteComponents: "ejs:npm"}).Download(context.Background(), "https://youtube.com/watch?v=1", t.TempDir())

	raw, err := os.ReadFile(argv)
	if err != nil {
		t.Fatalf("the fake yt-dlp never ran: %v", err)
	}
	if !strings.Contains(string(raw), "--remote-components\nejs:npm\n") {
		t.Errorf("yt-dlp argv = %q, want --remote-components ejs:npm from the configuration", raw)
	}
}

type firstSignInFailure struct {
	Environment string `json:"environment"`
	Run         string `json:"run"`
}

func botCheckedJar(t *testing.T) string {
	t.Helper()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "yt-dlp"), []byte("#!/bin/sh\necho \"ERROR: [youtube] x: Sign in to confirm you're not a bot.\" >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	jar := filepath.Join(t.TempDir(), "cookies.txt")
	if err := os.WriteFile(jar, []byte("# Netscape HTTP Cookie File\n.youtube.com\tTRUE\t/\tTRUE\t1999999999\tLOGIN_INFO\tl\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return jar
}

func readFirstSignInFailure(t *testing.T, jar string) *firstSignInFailure {
	t.Helper()
	raw, err := os.ReadFile(jar + ".session.json")
	if err != nil {
		t.Fatalf("session file next to the configured jar: %v", err)
	}
	var session struct {
		FirstSignInFailure *firstSignInFailure `json:"first_signin_failure"`
	}
	if err := json.Unmarshal(raw, &session); err != nil {
		t.Fatalf("session file %q: %v", raw, err)
	}
	return session.FirstSignInFailure
}

func TestYtDlpSearcher_RecordsADownloadsBotCheckUnderTheConfiguredEnvironment(t *testing.T) {
	jar := botCheckedJar(t)

	_, err := download.YtDlpSearcher(&config.Config{YtDLPCookieFile: jar, Env: "production"}).Download(context.Background(), "https://youtube.com/watch?v=1", t.TempDir())

	if err == nil {
		t.Fatal("Download() = nil, want the bot check's failure")
	}
	if f := readFirstSignInFailure(t, jar); f == nil || f.Environment != "production" || f.Run != "download" {
		t.Errorf("first sign-in failure = %+v, want the download's, recorded under environment production", f)
	}
}

func TestYtDlpSearcher_RecordsAJarsFirstBotCheckUnderTheConfiguredEnvironment(t *testing.T) {
	jar := botCheckedJar(t)
	searcher := download.YtDlpSearcher(&config.Config{YtDLPCookieFile: jar, Env: "staging"})

	_ = searcher.Canary(context.Background(), download.YouTubeCanary)

	if f := readFirstSignInFailure(t, jar); f == nil || f.Environment != "staging" || f.Run != "canary" {
		t.Errorf("first sign-in failure = %+v, want the canary's, recorded under environment staging", f)
	}
}

type recordingTransport struct {
	urls []string
}

func (r *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r.urls = append(r.urls, req.URL.String())
	body := "https://b.imgur.gg/abcde12345-song.mp3"
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{},
		Request:    req,
	}, nil
}

func TestLiveTiers_TheSavedLinkReadsFileHostPagesThroughTheInjectedTransport(t *testing.T) {
	transport := &recordingTransport{}
	tiers, _, err := download.LiveTiers(t.Context(), &config.Config{YtMusicEnabled: true, AcquisitionFileHostPageTimeout: time.Second}, transport)
	if err != nil {
		t.Fatalf("LiveTiers: %v", err)
	}
	link := tiers.First[0]
	if link.Name() != download.SavedLinkSourceName {
		t.Fatalf("first source = %s, want the saved link", link.Name())
	}

	candidates, err := link.Find(t.Context(), download.FindRequest{
		Title:          "Song",
		CatalogSources: []download.CatalogSource{{Provider: "imgurgg", ExternalID: "abcde12345"}},
	})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(transport.urls) != 1 || transport.urls[0] != "https://imgur.gg/f/abcde12345" {
		t.Errorf("requests through the injected transport = %v, want the one file host page", transport.urls)
	}
	if len(candidates) != 1 || candidates[0].URL != "https://b.imgur.gg/abcde12345-song.mp3" {
		t.Errorf("candidates = %+v, want the file found on the page", candidates)
	}
}

func sourceNames(sources []download.AudioSource) []download.SourceName {
	names := make([]download.SourceName, 0, len(sources))
	for _, s := range sources {
		names = append(names, s.Name())
	}
	return names
}

func containsSource(list []download.SourceName, want download.SourceName) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func TestCanaryYTMClient_BoundsEveryCall(t *testing.T) {
	client, err := download.InnerTubeClient(context.Background(), &config.Config{}, nil)
	if err != nil {
		t.Fatalf("InnerTubeClient without redis: %v", err)
	}

	if client.Timeout() <= 0 {
		t.Errorf("Timeout() = %s, want a positive bound on every InnerTube call", client.Timeout())
	}
}

func TestCanaryYTMClient_ACooldownAnotherInstanceRecordedInRedisHoldsItsSearches(t *testing.T) {
	redis := goredis.NewClient(&goredis.Options{Addr: miniredis.RunT(t).Addr()})
	t.Cleanup(func() { _ = redis.Close() })
	otherInstance, err := outbound.NewYouTubeMusicBudget(redis, outbound.YouTubeMusicBudgetSettings{Cooldown: time.Hour}, clock.Now, clock.Sleep)
	if err != nil {
		t.Fatalf("budget for the other instance: %v", err)
	}
	if err := otherInstance.CoolDown(t.Context()); err != nil {
		t.Fatalf("record the cooldown: %v", err)
	}
	client, err := download.InnerTubeClient(context.Background(), &config.Config{}, redis)
	if err != nil {
		t.Fatalf("InnerTubeClient with redis: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	_, err = client.SearchSongs(ctx, "fixture query")

	if !errors.Is(err, outboundtest.ErrYouTubeMusicBudgetRefused) {
		t.Errorf("SearchSongs during a shared cooldown: err = %v, want %v before any request leaves", err, outboundtest.ErrYouTubeMusicBudgetRefused)
	}
}

func innerTube(t *testing.T) *download.YouTubeMusicSearchClient {
	t.Helper()
	client, err := download.NewYtmSearchFor(&http.Client{Timeout: time.Second})
	if err != nil {
		t.Fatalf("build the InnerTube client: %v", err)
	}
	return client
}

func TestChecks_OnlyProbeEnabledSources(t *testing.T) {
	searcher := download.YtDlpSearcher(&config.Config{})
	tests := []struct {
		ytMusic, ytDlp bool
		want           string
	}{
		{true, true, "youtube,ytmusic,soundcloud,ytmsearch"},
		{false, true, "youtube,soundcloud"},
		{true, false, "ytmusic,ytmsearch"},
		{false, false, ""},
	}
	for _, tt := range tests {
		got := download.SourceCheckNames(searcher, true, innerTube(t), tt.ytMusic, tt.ytDlp)
		if got != tt.want {
			t.Errorf("checks(ytMusic=%v, ytDlp=%v) = %q, want %q", tt.ytMusic, tt.ytDlp, got, tt.want)
		}
	}
}

func TestChecks_ProbeInnerTubeWithoutYtDlp(t *testing.T) {
	for _, searcher := range []bool{false, true} {
		var got string
		if searcher {
			got = download.SourceCheckNames(download.YtDlpSearcher(&config.Config{}), false, innerTube(t), true, true)
		} else {
			got = download.SourceCheckNames(nil, true, innerTube(t), true, true)
		}
		if got != "ytmsearch" {
			t.Errorf("checks(searcher=%v, unavailable) = %q, want only ytmsearch", searcher, got)
		}
	}
	if got := download.SourceCheckNames(nil, false, nil, true, true); got != "" {
		t.Errorf("checks without any client = %q, want none", got)
	}
	if got := download.SourceCheckNames(nil, false, innerTube(t), false, true); got != "" {
		t.Errorf("checks with ytmusic disabled = %q, want none", got)
	}
}

func TestModule_ReportsSourceChecksOnlyWhenOneExists(t *testing.T) {
	for _, tt := range []struct {
		ytMusic bool
		want    bool
	}{{true, true}, {false, false}} {
		deps := offlineDeps(&config.Config{Env: "test", YtMusicEnabled: tt.ytMusic}, download.SourceOrder{})
		deps.AudioStore = nil

		module, err := download.New(t.Context(), deps)
		if err != nil {
			t.Fatalf("New: %v", err)
		}

		if got := module.HasSourceChecks(); got != tt.want {
			t.Errorf("HasSourceChecks with ytmusic=%v = %v, want %v", tt.ytMusic, got, tt.want)
		}
	}
}
