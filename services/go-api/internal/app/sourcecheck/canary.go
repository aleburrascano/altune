package sourcecheck

import (
	"altune/go-api/internal/download"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/logging"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

type Check struct {
	Name string
	Run  func(context.Context) error
}

type Probes struct {
	YtDlp          *download.YouTubeAudioSearcher
	YtDlpAvailable bool
	InnerTube      *download.YouTubeMusicSearchClient
	YtMusicEnabled bool
	YtDlpEnabled   bool
}

func ytDlpChecks(p Probes) []Check {
	if p.YtDlp == nil || !p.YtDlpAvailable {
		return nil
	}
	probe := func(source download.CanarySource) Check {
		return Check{Name: source.Name, Run: func(ctx context.Context) error { return p.YtDlp.Canary(ctx, source) }}
	}
	var checks []Check
	if p.YtDlpEnabled {
		checks = append(checks, probe(download.YouTubeCanary))
	}
	if p.YtMusicEnabled {
		checks = append(checks, probe(download.YTMusicCanary))
	}
	if p.YtDlpEnabled {
		checks = append(checks, probe(download.SoundCloudCanary))
	}
	return checks
}

func Checks(p Probes) []Check {
	checks := ytDlpChecks(p)
	if p.YtMusicEnabled && p.InnerTube != nil {
		checks = append(checks, Check{Name: string(download.YouTubeMusicSearchSourceName), Run: p.InnerTube.Canary})
	}
	return checks
}

func Run(ctx context.Context, now clock.Func, checks []Check) error {
	if now == nil {
		panic("sourcecheck.Run: a nil clock")
	}
	var dark []string
	for _, check := range checks {
		started := now()
		err := check.Run(ctx)
		logResult(ctx, check.Name, now().Sub(started), err)
		if err != nil {
			dark = append(dark, fmt.Sprintf("%s (%q)", check.Name, err.Error()))
		}
	}
	if len(dark) == 0 {
		return nil
	}
	return fmt.Errorf("sources dark: %s", strings.Join(dark, ", "))
}

func logResult(ctx context.Context, source string, duration time.Duration, err error) {
	if err != nil {
		slog.InfoContext(ctx, "acquisition.source_canary",
			"source", source, "ok", false, logging.KeyDurationMS, duration.Milliseconds(), logging.KeyError, err)
		return
	}
	slog.InfoContext(ctx, "acquisition.source_canary",
		"source", source, "ok", true, logging.KeyDurationMS, duration.Milliseconds())
}
