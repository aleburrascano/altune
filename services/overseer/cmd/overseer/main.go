package main

import (
	"altune/overseer/internal/app"
	"altune/overseer/internal/config"
	"context"
	"fmt"
	"log/slog"
	"os"

	_ "altune/overseer/internal/buckets/backendperf"
	_ "altune/overseer/internal/buckets/cost"
	_ "altune/overseer/internal/buckets/domainquality"
	_ "altune/overseer/internal/buckets/heartbeat"
	_ "altune/overseer/internal/buckets/liveactivity"
	_ "altune/overseer/internal/buckets/logs"
	_ "altune/overseer/internal/buckets/reliability"
	_ "altune/overseer/internal/buckets/security"
	_ "altune/overseer/internal/buckets/usage"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	app.SetupLogging(cfg.LogLevel, cfg.IsDevelopment())

	if err := app.New(cfg).Run(context.Background()); err != nil {
		slog.Error("overseer exited with error", "error", err)
		os.Exit(1)
	}
}
