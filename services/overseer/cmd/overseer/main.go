package main

import (
	"altune/overseer/internal/app"
	"altune/overseer/internal/config"
	"context"
	"fmt"
	"log/slog"
	"os"
)

func main() {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	app.SetupLogging(cfg.LogLevel, cfg.IsDevelopment())

	if err := app.New(cfg, app.Buckets(cfg, os.Getenv), os.Getenv).Run(context.Background()); err != nil {
		slog.Error("overseer exited with error", "error", err)
		os.Exit(1)
	}
}
