package audioacquisition_test

import (
	"altune/go-api/internal/acquisition"
	"altune/go-api/internal/app/audioacquisition"
	"altune/go-api/internal/shared/config"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

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
			sources, _ := audioacquisition.LiveTiers(t.Context(), &config.Config{YtMusicEnabled: tt.ytMusic, YtDLPEnabled: tt.ytDlp}, nil)
			names := sourceNames(sources.All())

			if got := containsSource(names, acquisition.YtMusicSourceName); got != tt.wantYtMusic {
				t.Errorf("ytmusic present = %v, want %v (sources=%v)", got, tt.wantYtMusic, names)
			}
			if got := containsSource(names, acquisition.YtDlpSourceName); got != tt.wantYtDlp {
				t.Errorf("ytdlp present = %v, want %v (sources=%v)", got, tt.wantYtDlp, names)
			}
		})
	}
}

func TestLiveTiers_YouTubeMusicSearchRunsFirstAndYtDlpIsTheFallback(t *testing.T) {
	tiers, _ := audioacquisition.LiveTiers(t.Context(), &config.Config{YtMusicEnabled: true, YtDLPEnabled: true}, nil)

	first, fallback := sourceNames(tiers.First), sourceNames(tiers.Fallback)
	if !containsSource(first, acquisition.YtMusicSourceName) || !containsSource(first, acquisition.YtmSearchSourceName) {
		t.Errorf("first tier = %v, want the catalog id and the songs search", first)
	}
	if len(fallback) != 1 || fallback[0] != acquisition.YtDlpSourceName {
		t.Errorf("fallback = %v, want only ytdlp", fallback)
	}
}

func TestLiveTiers_TheSongsSearchOutranksTheCatalogLinkForTheSameVideo(t *testing.T) {
	tiers, _ := audioacquisition.LiveTiers(t.Context(), &config.Config{YtMusicEnabled: true, YtDLPEnabled: true}, nil)
	first := sourceNames(tiers.First)

	if len(first) != 3 || first[0] != acquisition.CatalogPinSourceName || first[1] != acquisition.YtmSearchSourceName || first[2] != acquisition.YtMusicSourceName {
		t.Errorf("first tier = %v, want [%s %s %s]: duplicates keep the earlier source, and only the songs search carries the video's own length",
			first, acquisition.CatalogPinSourceName, acquisition.YtmSearchSourceName, acquisition.YtMusicSourceName)
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

			tiers, tools := audioacquisition.LiveTiers(t.Context(), &config.Config{YtDLPEnabled: true}, nil)

			if tools.YtDlp != tt.installed {
				t.Errorf("tools report yt-dlp installed = %v, want %v", tools.YtDlp, tt.installed)
			}
			if names := sourceNames(tiers.All()); !containsSource(names, acquisition.YtDlpSourceName) {
				t.Errorf("sources %v, want the yt-dlp source wired whether or not the binary is found", names)
			}
		})
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
	searcher := audioacquisition.YtDlpSearcher(&config.Config{YtDLPCookieFile: jar, Env: "staging"})

	_ = searcher.Canary(context.Background(), acquisition.YouTubeCanary)

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

func sourceNames(sources []acquisition.AudioSource) []acquisition.SourceName {
	names := make([]acquisition.SourceName, 0, len(sources))
	for _, s := range sources {
		names = append(names, s.Name())
	}
	return names
}

func containsSource(list []acquisition.SourceName, want acquisition.SourceName) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
