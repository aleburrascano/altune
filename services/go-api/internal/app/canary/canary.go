package canary

import (
	"altune/go-api/internal/acquisition"
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
	YtDlp          *acquisition.YtDlpAudioSearcher
	YtDlpAvailable bool
	InnerTube      *acquisition.YtmSearchClient
	YtMusicEnabled bool
	YtDlpEnabled   bool
}

func Checks(p Probes) []Check {
	var checks []Check
	if p.YtDlp != nil && p.YtDlpAvailable {
		probe := func(source acquisition.CanarySource) Check {
			return Check{Name: source.Name, Run: func(ctx context.Context) error { return p.YtDlp.Canary(ctx, source) }}
		}
		if p.YtDlpEnabled {
			checks = append(checks, probe(acquisition.YouTubeCanary))
		}
		if p.YtMusicEnabled {
			checks = append(checks, probe(acquisition.YTMusicCanary))
		}
		if p.YtDlpEnabled {
			checks = append(checks, probe(acquisition.SoundCloudCanary))
		}
	}
	if p.YtMusicEnabled && p.InnerTube != nil {
		checks = append(checks, Check{Name: string(acquisition.YtmSearchSourceName), Run: p.InnerTube.Canary})
	}
	return checks
}

func Run(ctx context.Context, checks []Check) error {
	var dark []string
	for _, check := range checks {
		started := time.Now()
		err := check.Run(ctx)
		logResult(ctx, check.Name, time.Since(started), err)
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
			"source", source, "ok", false, "duration", duration.String(), "error", err)
		return
	}
	slog.InfoContext(ctx, "acquisition.source_canary",
		"source", source, "ok", true, "duration", duration.String())
}
