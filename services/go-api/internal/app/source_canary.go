package app

import (
	"altune/go-api/internal/acquisition"
	"altune/go-api/internal/app/jobs"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

const sourceCanaryInterval = time.Hour

type namedCanary struct {
	name string
	run  func(context.Context) error
}

func (a *App) startSourceCanary(ctx context.Context, searcher *acquisition.YtDlpAudioSearcher, ytDlpAvailable bool, ytm *acquisition.YtmSearchClient, enabled sourceToggles) {
	canaries := sourceCanaries(searcher, ytDlpAvailable, ytm, enabled)
	if len(canaries) == 0 {
		return
	}
	a.startSimpleJob(jobs.AcquisitionSourceCanary, sourceCanaryInterval, func(ctx context.Context) error {
		return runSourceCanaries(ctx, canaries)
	}, "interval", sourceCanaryInterval.String())
}

type sourceToggles struct {
	ytMusic bool
	ytDlp   bool
}

func sourceCanaries(searcher *acquisition.YtDlpAudioSearcher, ytDlpAvailable bool, ytm *acquisition.YtmSearchClient, enabled sourceToggles) []namedCanary {
	var canaries []namedCanary
	if searcher != nil && ytDlpAvailable {
		probe := func(source acquisition.CanarySource) namedCanary {
			return namedCanary{name: source.Name, run: func(ctx context.Context) error { return searcher.Canary(ctx, source) }}
		}
		if enabled.ytDlp {
			canaries = append(canaries, probe(acquisition.YouTubeCanary))
		}
		if enabled.ytMusic {
			canaries = append(canaries, probe(acquisition.YTMusicCanary))
		}
		if enabled.ytDlp {
			canaries = append(canaries, probe(acquisition.SoundCloudCanary))
		}
	}
	if enabled.ytMusic && ytm != nil {
		canaries = append(canaries, namedCanary{name: string(acquisition.YtmSearchSourceName), run: ytm.Canary})
	}
	return canaries
}

func runSourceCanaries(ctx context.Context, canaries []namedCanary) error {
	var dark []string
	for _, canary := range canaries {
		started := time.Now()
		err := canary.run(ctx)
		logSourceCanaryResult(ctx, canary.name, time.Since(started), err)
		if err != nil {
			dark = append(dark, darkSourceMessage(canary.name, err))
		}
	}
	if len(dark) == 0 {
		return nil
	}
	return fmt.Errorf("sources dark: %s", strings.Join(dark, ", "))
}

func darkSourceMessage(source string, err error) string {
	return fmt.Sprintf("%s (%q)", source, err.Error())
}

func logSourceCanaryResult(ctx context.Context, source string, duration time.Duration, err error) {
	if err != nil {
		slog.InfoContext(ctx, "acquisition.source_canary",
			"source", source, "ok", false, "duration", duration.String(), "error", err)
		return
	}
	slog.InfoContext(ctx, "acquisition.source_canary",
		"source", source, "ok", true, "duration", duration.String())
}
