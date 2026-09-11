package main

import (
	"context"
	"flag"
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

	logRing := logging.Setup(cfg.LogLevel, cfg.IsDevelopment())

	if len(os.Args) < 2 {
		runServer(cfg, logRing)
		return
	}

	cmd, args := os.Args[1], os.Args[2:]
	switch cmd {
	case "serve":
		runServer(cfg, logRing)
	case "migrate-dedup":
		commands.RunDedupMigration(cfg, parseExecute(cmd, args))
	case "health-check":
		commands.RunHealthCheck(cfg, parseFix(cmd, args))
	case "fix-audio-refs":
		commands.RunFixAudioRefs(cfg, parseExecute(cmd, args))
	case "backfill-duration":
		commands.RunBackfillDuration(cfg, parseExecute(cmd, args))
	case "reconcile-truncated":
		commands.RunReconcileTruncated(cfg, parseExecute(cmd, args))
	case "backfill-m4a":
		execute, limit := parseExecuteLimit(cmd, args)
		commands.RunBackfillM4a(cfg, execute, limit)
	case "reacquire-corrupt-m4a":
		execute, limit := parseExecuteLimit(cmd, args)
		commands.RunReacquireCorruptM4a(cfg, execute, limit)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\nUsage: api [serve|migrate-dedup|health-check|fix-audio-refs|backfill-duration|reconcile-truncated|backfill-m4a|reacquire-corrupt-m4a]\n", cmd)
		os.Exit(1)
	}
}

func runServer(cfg *config.Config, logRing *logging.RingBuffer) {
	a := app.New(cfg, logRing)
	if err := a.Run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
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

func parseExecuteLimit(name string, args []string) (bool, int) {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	execute := fs.Bool("execute", false, "apply changes instead of a dry-run")
	limit := fs.Int("limit", 0, "cap rows touched (<= 0 means all)")
	_ = fs.Parse(args)
	return *execute, *limit
}
