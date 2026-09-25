package app

import (
	"altune/go-api/internal/acquisition/adapters/ytdlp"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

const sourceCanaryInterval = time.Hour

type sourceCanaryProbe interface {
	Canary(ctx context.Context, source ytdlp.CanarySource) error
}

func (a *App) startSourceCanary(ctx context.Context, searcher *ytdlp.YtDlpAudioSearcher, ytDlpAvailable bool, enabled sourceToggles) {
	if searcher == nil || !ytDlpAvailable {
		return
	}
	canaries := sourceCanaries(enabled)
	if len(canaries) == 0 {
		return
	}
	probe := sourceCanaryProbe(searcher)
	a.startSimpleJob(ctx, jobAcquisitionSourceCanary, sourceCanaryInterval, func(ctx context.Context) error {
		return runSourceCanaries(ctx, probe, canaries)
	}, "interval", sourceCanaryInterval.String())
}

type sourceToggles struct {
	ytMusic bool
	ytDlp   bool
}

func sourceCanaries(enabled sourceToggles) []ytdlp.CanarySource {
	var canaries []ytdlp.CanarySource
	if enabled.ytDlp {
		canaries = append(canaries, ytdlp.YouTubeCanary)
	}
	if enabled.ytMusic {
		canaries = append(canaries, ytdlp.YTMusicCanary)
	}
	if enabled.ytDlp {
		canaries = append(canaries, ytdlp.SoundCloudCanary)
	}
	return canaries
}

func runSourceCanaries(ctx context.Context, probe sourceCanaryProbe, canaries []ytdlp.CanarySource) error {
	var dark []string
	for _, canary := range canaries {
		started := time.Now()
		err := probe.Canary(ctx, canary)
		logSourceCanaryResult(ctx, canary.Name, time.Since(started), err)
		if err != nil {
			dark = append(dark, darkSourceMessage(canary.Name, err))
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
