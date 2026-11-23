package app_test

import (
	"altune/go-api/internal/app"
	"altune/go-api/internal/app/jobs"
	"altune/go-api/internal/shared/config"
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func assembledJobStarted(t *testing.T, cfg *config.Config, name jobs.Name) bool {
	t.Helper()
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	ctx, cancel := context.WithCancel(t.Context())
	assembly, err := app.Assemble(ctx, cfg, app.Handles{Pool: unreachablePool(t), Transport: refuseEgress{}})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		assembly.Shutdown()
	})
	return strings.Contains(logs.String(), `msg="`+string(name)+` started"`)
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
