package main

import (
	"context"
	"fmt"
	"os"

	"altune/go-api/internal/app"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/logging"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	logging.Setup(cfg)

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "serve":
			runServer(cfg)
		case "migrate-dedup":
			fmt.Println("dedup migration: not yet implemented")
		case "health-check":
			fmt.Println("health check: not yet implemented")
		case "fix-audio-refs":
			fmt.Println("fix audio refs: not yet implemented")
		default:
			fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
			os.Exit(1)
		}
	} else {
		runServer(cfg)
	}
}

func runServer(cfg *config.Config) {
	a := app.New(cfg)
	if err := a.Run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}
