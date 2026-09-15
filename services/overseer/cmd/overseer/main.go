// Command overseer is the standalone Overseer service: a god's-eye control room
// for the single owner that watches the Altune app across its public surface and
// presents it as self-contained bucket plugins. See docs/overseer.md (what) and
// docs/overseer-design.md (how).
package main

import (
	"altune/overseer/internal/app"
	"altune/overseer/internal/config"
	"context"
	"fmt"
	"log/slog"
	"os"

	// Bucket registrations. Each bucket self-registers in its package init; a new
	// bucket is activated by adding exactly one blank import line here and its own
	// files — the additive-buckets invariant.
	_ "altune/overseer/internal/buckets/domainquality"
	_ "altune/overseer/internal/buckets/heartbeat"
	_ "altune/overseer/internal/buckets/liveactivity"
	_ "altune/overseer/internal/buckets/logs"
	_ "altune/overseer/internal/buckets/reliability"
	_ "altune/overseer/internal/buckets/stub"
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
