package commands

import (
	"altune/go-api/cmd/api/commands/orphans"
	"altune/go-api/internal/library"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/telemetry"
	"context"
	"fmt"
	"log/slog"
)

func RunHealthCheck(cfg *config.Config, mode RunMode) error {
	return runHealthCheck(cfg, mode)
}

func runHealthCheck(cfg *config.Config, mode RunMode) error {
	ctx := context.Background()
	pool, err := openPool(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	return healthCheck(ctx, cfg, library.NewPostgresTrackRepository(pool, library.NewDBCallMetrics(telemetry.NewRegistry())), mode)
}

func healthCheck(ctx context.Context, cfg *config.Config, repo *library.PostgresTrackRepository, mode RunMode) error {
	tracks, err := repo.ListReadyAudioTracks(ctx)
	if err != nil {
		return fmt.Errorf("list ready audio tracks: %w", err)
	}
	totalChecked := len(tracks)
	fmt.Printf("\nChecking %d tracks with status=ready...\n\n", totalChecked)

	audioStore, err := library.NewAudioStoreFromConfig(cfg, slog.Default(), library.NewFSCallMetrics(telemetry.NewRegistry()))
	if err != nil {
		return fmt.Errorf("call failed: %w", err)
	}

	orphaned := orphans.Check(ctx, tracks, audioStore)
	if mode != Apply {
		reportHealthCheck(ctx, totalChecked, len(orphaned), 0, mode)
		return nil
	}
	fixed := orphans.Fix(ctx, orphaned, repo)
	reportHealthCheck(ctx, totalChecked, len(orphaned), fixed, mode)
	if err := orphans.RepairOutcome(len(orphaned), fixed); err != nil {
		return fmt.Errorf("repair outcome: %w", err)
	}
	return nil
}

func reportHealthCheck(ctx context.Context, totalChecked, orphanedDB, fixed int, mode RunMode) {
	printSummary("Health check complete:")
	fmt.Printf("  Tracks checked:    %d\n", totalChecked)
	fmt.Printf("  Orphaned DB rows:  %d\n", orphanedDB)
	if mode == Apply {
		fmt.Printf("  Fixed:             %d\n", fixed)
	} else if orphanedDB > 0 {
		fmt.Println("\n  Run with --fix to mark orphaned tracks as failed.")
	}
	fmt.Println()

	slog.InfoContext(ctx, "health_check_completed",
		"total_checked", totalChecked,
		"orphaned_db", orphanedDB,
		"fixed", fixed)
}
