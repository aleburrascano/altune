package commands

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/database"

	"github.com/jackc/pgx/v5/pgxpool"
)

func RunHealthCheck(cfg *config.Config, fix bool) {
	if cfg.DatabaseURL == "" {
		fmt.Println("ERROR: DATABASE_URL not set")
		os.Exit(1)
	}

	ctx := context.Background()
	pool, err := database.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		fmt.Printf("ERROR: database connection failed: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()

	orphanedDB := 0
	fixed := 0
	totalChecked := 0

	rows, err := pool.Query(ctx,
		`SELECT id, user_id, title, artist, audio_ref
		FROM tracks WHERE acquisition_status = 'ready' AND audio_ref IS NOT NULL`)
	if err != nil {
		fmt.Printf("ERROR: query failed: %v\n", err)
		os.Exit(1)
	}
	defer rows.Close()

	type trackRow struct {
		id, userId, title, artist, audioRef string
	}
	var tracks []trackRow
	for rows.Next() {
		var t trackRow
		if err := rows.Scan(&t.id, &t.userId, &t.title, &t.artist, &t.audioRef); err != nil {
			fmt.Printf("ERROR: scan failed: %v\n", err)
			os.Exit(1)
		}
		tracks = append(tracks, t)
	}
	totalChecked = len(tracks)
	fmt.Printf("\nChecking %d tracks with status=ready...\n\n", totalChecked)

	audioStore := buildAudioStoreForCLI(cfg)

	for _, t := range tracks {
		exists, err := audioStore.Exists(ctx, t.audioRef)
		if err != nil || !exists {
			orphanedDB++
			fmt.Printf("  ORPHANED: %s — %s  (id=%s, ref=%s)\n", t.title, t.artist, t.id, t.audioRef)

			if fix {
				markTrackFailed(ctx, pool, t.id, t.userId)
				fixed++
				fmt.Println("    → FIXED: marked as failed")
			}
		}
	}

	fmt.Printf("\n%s\n", "==================================================")
	fmt.Println("Health check complete:")
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

func markTrackFailed(ctx context.Context, pool *pgxpool.Pool, trackID, userID string) {
	_, _ = pool.Exec(ctx,
		`UPDATE tracks SET acquisition_status = 'failed',
			failure_reason = 'Audio file missing from storage (health-check)',
			audio_ref = NULL
		WHERE id = $1 AND user_id = $2`,
		trackID, userID)
}
