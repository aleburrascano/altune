package app_test

import (
	"altune/go-api/internal/app/jobs"
	"altune/go-api/internal/shared/config"
	"strings"
	"testing"
)

func assembledJobStarted(t *testing.T, cfg *config.Config, name jobs.Name) bool {
	t.Helper()
	return strings.Contains(assembleLogging(t, cfg).logs.String(), `msg="`+string(name)+` started"`)
}

func TestSourceCanary_NoJobWhenEveryCanariedSourceIsDisabled(t *testing.T) {
	cfg := jwksConfig(t)
	cfg.YtMusicEnabled, cfg.YtDLPEnabled = false, false

	if assembledJobStarted(t, cfg, jobs.AcquisitionSourceCanary) {
		t.Fatal("the source canary job was registered although every canaried source is disabled")
	}
}

func TestSourceCanary_RegisteredWhenYouTubeMusicIsEnabled(t *testing.T) {
	cfg := jwksConfig(t)
	cfg.YtMusicEnabled = true

	if !assembledJobStarted(t, cfg, jobs.AcquisitionSourceCanary) {
		t.Fatal("the source canary job was not registered although YouTube Music, and so its InnerTube probe, is enabled")
	}
}
