package downloadsetup_test

import (
	"altune/go-api/internal/app/downloadsetup"
	"altune/go-api/internal/download"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/youtubemusicapi"
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
	goredis "github.com/redis/go-redis/v9"
)

func liveTiers(t *testing.T, cfg *config.Config) (downloadsetup.SourceOrder, download.Verification) {
	t.Helper()
	tiers, tools, err := downloadsetup.EnabledSources(t.Context(), cfg, nil, http.DefaultTransport)
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

	_, _ = downloadsetup.YtDlpSearcher(&config.Config{YtDLPJSRuntime: "node", YtDLPRemoteComponents: "ejs:npm"}).Download(context.Background(), "https://youtube.com/watch?v=1", t.TempDir())

	raw, err := os.ReadFile(argv)
	if err != nil {
		t.Fatalf("the fake yt-dlp never ran: %v", err)
	}
	if !strings.Contains(string(raw), "--remote-components\nejs:npm\n") {
		t.Errorf("yt-dlp argv = %q, want --remote-components ejs:npm from the configuration", raw)
	}
}

func TestYtDlpSearcher_RecordsADownloadsBotCheckUnderTheConfiguredEnvironment(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "yt-dlp"), []byte("#!/bin/sh\necho \"ERROR: [youtube] x: Sign in to confirm you're not a bot.\" >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	jar := filepath.Join(t.TempDir(), "cookies.txt")
	if err := os.WriteFile(jar, []byte("# Netscape HTTP Cookie File\n.youtube.com\tTRUE\t/\tTRUE\t1999999999\tLOGIN_INFO\tl\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := downloadsetup.YtDlpSearcher(&config.Config{YtDLPCookieFile: jar, Env: "production"}).Download(context.Background(), "https://youtube.com/watch?v=1", t.TempDir())

	if err == nil {
		t.Fatal("Download() = nil, want the bot check's failure")
	}
	raw, readErr := os.ReadFile(jar + ".session.json")
	if readErr != nil {
		t.Fatalf("session file next to the live jar: %v", readErr)
	}
	var session struct {
		FirstSignInFailure *struct {
			Environment string `json:"environment"`
			Run         string `json:"run"`
		} `json:"first_signin_failure"`
	}
	if err := json.Unmarshal(raw, &session); err != nil {
		t.Fatalf("session file %q: %v", raw, err)
	}
	if f := session.FirstSignInFailure; f == nil || f.Environment != "production" || f.Run != "download" {
		t.Errorf("first sign-in failure = %+v, want the download's, recorded under environment production", f)
	}
}

func TestYtDlpSearcher_RecordsAJarsFirstBotCheckUnderTheConfiguredEnvironment(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "yt-dlp"), []byte("#!/bin/sh\necho \"ERROR: [youtube] x: Sign in to confirm you're not a bot.\" >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	jar := filepath.Join(t.TempDir(), "cookies.txt")
	if err := os.WriteFile(jar, []byte("# Netscape HTTP Cookie File\n.youtube.com\tTRUE\t/\tTRUE\t1999999999\tLOGIN_INFO\tl\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	searcher := downloadsetup.YtDlpSearcher(&config.Config{YtDLPCookieFile: jar, Env: "staging"})

	_ = searcher.Canary(context.Background(), download.YouTubeCanary)

	raw, err := os.ReadFile(jar + ".session.json")
	if err != nil {
		t.Fatalf("session file next to the configured jar: %v", err)
	}
	var session struct {
		FirstSignInFailure *struct {
			Environment string `json:"environment"`
			Run         string `json:"run"`
		} `json:"first_signin_failure"`
	}
	if err := json.Unmarshal(raw, &session); err != nil {
		t.Fatalf("session file %q: %v", raw, err)
	}
	if f := session.FirstSignInFailure; f == nil || f.Environment != "staging" || f.Run != "canary" {
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
	tiers, _, err := downloadsetup.EnabledSources(t.Context(), &config.Config{YtMusicEnabled: true, AcquisitionFileHostPageTimeout: time.Second}, nil, transport)
	if err != nil {
		t.Fatalf("EnabledSources: %v", err)
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
	client, err := downloadsetup.CanaryYTMClient(&config.Config{}, nil, http.DefaultTransport)
	if err != nil {
		t.Fatalf("CanaryYTMClient without redis: %v", err)
	}

	if client.Timeout() <= 0 {
		t.Errorf("Timeout() = %s, want a positive bound on every InnerTube call", client.Timeout())
	}
}

func TestCanaryYTMClient_ACooldownAnotherInstanceRecordedInRedisHoldsItsSearches(t *testing.T) {
	redis := goredis.NewClient(&goredis.Options{Addr: miniredis.RunT(t).Addr()})
	t.Cleanup(func() { _ = redis.Close() })
	otherInstance, err := youtubemusicapi.NewBudget(redis, youtubemusicapi.Settings{Cooldown: time.Hour}, clock.Now, clock.Sleep)
	if err != nil {
		t.Fatalf("budget for the other instance: %v", err)
	}
	if err := otherInstance.CoolDown(t.Context()); err != nil {
		t.Fatalf("record the cooldown: %v", err)
	}
	client, err := downloadsetup.CanaryYTMClient(&config.Config{}, redis, http.DefaultTransport)
	if err != nil {
		t.Fatalf("CanaryYTMClient with redis: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	_, err = client.SearchSongs(ctx, "fixture query")

	if !errors.Is(err, youtubemusicapi.ErrBudgetRefused) {
		t.Errorf("SearchSongs during a shared cooldown: err = %v, want %v before any request leaves", err, youtubemusicapi.ErrBudgetRefused)
	}
}
