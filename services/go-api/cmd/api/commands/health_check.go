package commands

import (
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

	orphanedDB := 0
	fixed := 0

	tracks, err := loadReadyTracks(ctx, pool, "", "")
	if err != nil {
		return err
	}
	totalChecked := len(tracks)
	fmt.Printf("\nChecking %d tracks with status=ready...\n\n", totalChecked)

	audioStore, err := NewAudioStoreFromConfig(cfg)
	if err != nil {
		return err
	}

	for _, t := range tracks {
		exists, err := audioStore.Exists(ctx, t.AudioRef)
		if err != nil || !exists {
			orphanedDB++
			fmt.Printf("  ORPHANED: %s — %s  (id=%s, ref=%s)\n", t.Title, t.Artist, t.Id, t.AudioRef)

			if fix {
				_ = markTrackFailed(ctx, pool, t.Id, t.UserId, "Audio file missing from storage (health-check)")
				fixed++
				fmt.Println("    → FIXED: marked as failed")
			}
		}
	}

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
	return nil
}
