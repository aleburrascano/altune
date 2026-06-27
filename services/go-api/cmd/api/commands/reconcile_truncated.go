package commands

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/database"
)

// truncatedAudioThresholdSecs is the duration below which a stored file is treated
// as a truncated preview rather than a full track. SoundCloud previews are ~30s;
// the affected tracks all probe at 29.8s. No real track this library cares about
// is under 45s, so anything shorter is re-acquired rather than accepted.
const truncatedAudioThresholdSecs = 45.0

// RunReconcileTruncated repairs the tracks broken by the old SoundCloud
// direct-acquire path. For every ready track with a NULL duration, it probes the
// actual stored audio:
//   - probed >= threshold  → the audio is fine, only the duration was never
//     written → backfill duration_seconds in place (no re-download).
//   - probed <  threshold  → only a ~30s preview was stored → mark the track
//     failed + clear audio_ref so the app shows a retry, which re-acquires the
//     full track via the search pipeline.
//
// Dry-run by default; pass execute=true to apply.
func RunReconcileTruncated(cfg *config.Config, execute bool) {
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

	audioStore := buildAudioStoreForCLI(cfg)
	if audioStore == nil {
		fmt.Println("ERROR: no audio store configured (need MUSIC_DIR or OCI_S3_* env vars)")
		os.Exit(1)
	}

	rows, err := pool.Query(ctx,
		`SELECT id, user_id, title, artist, audio_ref
		FROM tracks
		WHERE duration_seconds IS NULL
		  AND audio_ref IS NOT NULL
		  AND acquisition_status = 'ready'
		ORDER BY added_at DESC`)
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

	fmt.Printf("\nFound %d ready tracks with missing duration...\n\n", len(tracks))
	if len(tracks) == 0 {
		fmt.Println("Nothing to do.")
		return
	}

	reacquired, backfilled, skipped, errored := 0, 0, 0, 0

	for i, t := range tracks {
		duration, err := probeDuration(ctx, audioStore, t.audioRef)
		if err != nil {
			fmt.Printf("  [%d/%d] SKIP: %s — %s  (probe error: %v)\n", i+1, len(tracks), t.title, t.artist, err)
			skipped++
			continue
		}

		truncated := duration < truncatedAudioThresholdSecs
		action := "BACKFILL duration"
		if truncated {
			action = "RE-ACQUIRE (truncated)"
		}
		fmt.Printf("  [%d/%d] %.1fs  %s — %s  → %s\n", i+1, len(tracks), duration, t.title, t.artist, action)

		if !execute {
			continue
		}

		if truncated {
			_, err = pool.Exec(ctx,
				`UPDATE tracks SET acquisition_status = 'failed',
					failure_reason = 'Only a short preview was downloaded — retry to re-acquire',
					audio_ref = NULL
				WHERE id = $1 AND user_id = $2`,
				t.id, t.userId)
			if err == nil {
				reacquired++
			}
		} else {
			_, err = pool.Exec(ctx,
				`UPDATE tracks SET duration_seconds = $3 WHERE id = $1 AND user_id = $2`,
				t.id, t.userId, duration)
			if err == nil {
				backfilled++
			}
		}
		if err != nil {
			fmt.Printf("    ERROR updating: %v\n", err)
			errored++
		}
	}

	fmt.Printf("\n%s\n", "==================================================")
	fmt.Println("Reconcile truncated complete:")
	fmt.Printf("  Total candidates:  %d\n", len(tracks))
	fmt.Printf("  Probe skipped:     %d\n", skipped)
	if execute {
		fmt.Printf("  Re-acquired:       %d  (marked failed → user taps retry)\n", reacquired)
		fmt.Printf("  Backfilled:        %d  (duration written in place)\n", backfilled)
		fmt.Printf("  Errors:            %d\n", errored)
	} else {
		fmt.Println("\n  Run with --execute to apply changes.")
	}
	fmt.Println()

	slog.Info("reconcile_truncated_completed",
		"total", len(tracks),
		"skipped", skipped,
		"reacquired", reacquired,
		"backfilled", backfilled,
		"errored", errored)
}
