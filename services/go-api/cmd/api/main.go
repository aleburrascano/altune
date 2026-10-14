package main

import (
	"altune/go-api/cmd/api/commands"
	"altune/go-api/internal/app"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/telemetry"
	"context"
	"flag"
	"fmt"
	"os"
	"time"
)

const telemetryFlushTimeout = 5 * time.Second

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	logRing := logging.Setup(cfg.LogLevel, cfg.IsDevelopment())

	if len(os.Args) < 2 {
		runServer(cfg, logRing)
		return
	}

	cmd, args := os.Args[1], os.Args[2:]
	switch cmd {
	case "serve":
		runServer(cfg, logRing)
	case "health-check":
		commands.RunHealthCheck(cfg, parseFix(cmd, args))
	case "journey-check":
		commands.RunJourneyCheck(cfg)
	case "sweep-staging-audio":
		commands.RunSweepStagingAudio(cfg, parseExecute(cmd, args))
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\nUsage: api [serve|health-check|journey-check|sweep-staging-audio]\n", cmd)
		os.Exit(1)
	}
}

func runServer(cfg *config.Config, logRing *logging.RingBuffer) {
	shutdown, err := telemetry.Setup(context.Background(), telemetry.Service{Name: "altune-api", Version: app.BuildCommit(), Environment: cfg.Env})
	if err != nil {
		fmt.Fprintf(os.Stderr, "telemetry setup: %v\n", err)
		os.Exit(1)
	}
	a := app.New(cfg, logRing)
	runErr := a.Run(context.Background())
	flushCtx, cancel := context.WithTimeout(context.Background(), telemetryFlushTimeout)
	defer cancel()
	if err := shutdown(flushCtx); err != nil {
		fmt.Fprintf(os.Stderr, "telemetry flush: %v\n", err)
	}
	if runErr != nil {
		fmt.Fprintf(os.Stderr, "server error: %v\n", runErr)
		cancel()
		os.Exit(1)
	}
}

func parseExecute(name string, args []string) bool {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	execute := fs.Bool("execute", false, "apply changes instead of a dry-run")
	_ = fs.Parse(args)
	return *execute
}

func parseFix(name string, args []string) bool {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	fix := fs.Bool("fix", false, "apply fixes instead of a dry-run")
	_ = fs.Parse(args)
	return *fix
}
