package commands

import (
	"altune/go-api/cmd/api/commands/orphans"
	"altune/go-api/internal/library"
	"altune/go-api/internal/shared/config"
	"context"
	"fmt"
	"log/slog"
)

func RunHealthCheck(cfg *config.Config, fix bool) {
	exitOnError(runHealthCheck(cfg, fix))
}

func runHealthCheck(cfg *config.Config, fix bool) error {
	ctx := context.Background()
	pool, err := openPool(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	return healthCheck(ctx, cfg, library.NewPostgresTrackRepository(pool), fix)
}

func healthCheck(ctx context.Context, cfg *config.Config, repo *library.PostgresTrackRepository, fix bool) error {
	tracks, err := repo.ListReadyAudioTracks(ctx)
	if err != nil {
		return err
	}
	totalChecked := len(tracks)
	fmt.Printf("\nChecking %d tracks with status=ready...\n\n", totalChecked)

	audioStore, err := library.NewAudioStoreFromConfig(cfg, slog.Default())
	if err != nil {
		return err
	}

	orphanedDB, fixed := orphans.Check(ctx, tracks, audioStore, repo, fix)
	reportHealthCheck(totalChecked, orphanedDB, fixed, fix)
	return nil
}

func reportHealthCheck(totalChecked, orphanedDB, fixed int, fix bool) {
	printSummary("Health check complete:")
	fmt.Printf("  Tracks checked:    %d\n", totalChecked)
	fmt.Printf("  Orphaned DB rows:  %d\n", orphanedDB)
	if fix {
		fmt.Printf("  Fixed:             %d\n", fixed)
	} else if orphanedDB > 0 {
		fmt.Println("\n  Run with --fix to mark orphaned tracks as failed.")
	}
	fmt.Println()

	slog.Info("health_check_completed",
		"total_checked", totalChecked,
		"orphaned_db", orphanedDB,
		"fixed", fixed)
}
