package main

import (
	"context"
	"fmt"
	"os"

	"altune/go-api/internal/app"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/logging"

	"altune/go-api/cmd/api/commands"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	logging.Setup(cfg)

	if len(os.Args) < 2 {
		runServer(cfg)
		return
	}

	switch os.Args[1] {
	case "serve":
		runServer(cfg)
	case "migrate-dedup":
		execute := hasFlag("--execute")
		commands.RunDedupMigration(cfg, execute)
	case "health-check":
		fix := hasFlag("--fix")
		commands.RunHealthCheck(cfg, fix)
	case "fix-audio-refs":
		execute := hasFlag("--execute")
		commands.RunFixAudioRefs(cfg, execute)
	case "backfill-duration":
		execute := hasFlag("--execute")
		commands.RunBackfillDuration(cfg, execute)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\nUsage: api [serve|migrate-dedup|health-check|fix-audio-refs|backfill-duration]\n", os.Args[1])
		os.Exit(1)
	}
}

func runServer(cfg *config.Config) {
	a := app.New(cfg)
	if err := a.Run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}

func hasFlag(flag string) bool {
	for _, arg := range os.Args[2:] {
		if arg == flag {
			return true
		}
	}
	return false
}
