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

	setupLogging := logging.Setup
	if cfg.IsDevelopment() {
		setupLogging = logging.SetupDevelopment
	}
	logRing := setupLogging(cfg.LogLevel)

	if err := dispatch(cfg, logRing, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func dispatch(cfg *config.Config, logRing *logging.RingBuffer, argv []string) error {
	if len(argv) == 0 {
		return runServer(cfg, logRing)
	}
	cmd, args := argv[0], argv[1:]
	switch cmd {
	case "serve":
		return runServer(cfg, logRing)
	case "health-check":
		return reportCommand(commands.RunHealthCheck(cfg, runMode(cmd, args, "fix")))
	case "journey-check":
		return reportCommand(commands.RunJourneyCheck(cfg))
	case "sweep-staging-audio":
		return reportCommand(commands.RunSweepStagingAudio(cfg, runMode(cmd, args, "execute")))
	default:
		return fmt.Errorf("unknown command: %s\nUsage: api [serve|health-check|journey-check|sweep-staging-audio]", cmd)
	}
}

func reportCommand(err error) error {
	if err != nil {
		return fmt.Errorf("ERROR: %w", err)
	}
	return nil
}

func runServer(cfg *config.Config, logRing *logging.RingBuffer) error {
	shutdown, err := telemetry.Setup(context.Background(), telemetry.Service{Name: "altune-api", Version: app.BuildCommit(), Environment: cfg.Env}, os.Getenv)
	if err != nil {
		return fmt.Errorf("telemetry setup: %w", err)
	}
	a := app.New(cfg, logRing)
	runErr := a.Run(context.Background())
	flushCtx, cancel := context.WithTimeout(context.Background(), telemetryFlushTimeout)
	flushErr := shutdown(flushCtx)
	cancel()
	if flushErr != nil {
		fmt.Fprintf(os.Stderr, "telemetry flush: %v\n", flushErr)
	}
	if runErr != nil {
		return fmt.Errorf("server error: %w", runErr)
	}
	return nil
}

func runMode(name string, args []string, flagName string) commands.RunMode {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	apply := fs.Bool(flagName, false, "apply changes instead of a dry-run")
	_ = fs.Parse(args)
	if *apply {
		return commands.Apply
	}
	return commands.DryRun
}
