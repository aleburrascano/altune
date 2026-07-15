package commands

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"altune/go-api/internal/acquisition/adapters/ytdlp"
	acqService "altune/go-api/internal/acquisition/service"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/database"
)

// RunReacquireCorruptM4a re-acquires every ready track whose stored audio is a
// .m4a — the corrupt files the ID3-on-m4a pipeline produced before it was reverted
// to MP3 — replacing each with a fresh MP3 through the now-gated acquisition
// pipeline (search → select → download → decode-gate → tag → store-gate). Ordering
// mirrors backfill-m4a: store the new file → swap audio_ref → delete the old one,
// so a failed or gate-rejected re-acquire leaves the (already-broken) original in
// place rather than losing the row.
//
// Dry-run by default; execute=true applies. limit<=0 processes all.
func RunReacquireCorruptM4a(cfg *config.Config, execute bool, limit int) {
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
	searcher := ytdlp.NewYtDlpAudioSearcher(cfg.FFmpegLocation, cfg.YtDLPCookieFile, cfg.YtDLPJSRuntime)
	prober := ytdlp.NewFfprobeProber(cfg.FFmpegLocation)

	rows, err := pool.Query(ctx,
		`SELECT id, user_id, title, artist, album, audio_ref, duration_seconds
		FROM tracks
		WHERE acquisition_status = 'ready'
		  AND audio_ref IS NOT NULL
		  AND audio_ref LIKE '%.m4a'
		ORDER BY added_at DESC`)
	if err != nil {
		fmt.Printf("ERROR: query failed: %v\n", err)
		os.Exit(1)
	}
	defer rows.Close()

	type trackRow struct {
		id, userId, title, artist, oldRef string
		album                             *string
		duration                          *float64
	}
	var tracks []trackRow
	for rows.Next() {
		var t trackRow
		if err := rows.Scan(&t.id, &t.userId, &t.title, &t.artist, &t.album, &t.oldRef, &t.duration); err != nil {
			fmt.Printf("ERROR: scan failed: %v\n", err)
			os.Exit(1)
		}
		tracks = append(tracks, t)
	}
	if limit > 0 && len(tracks) > limit {
		tracks = tracks[:limit]
	}

	fmt.Printf("\nFound %d ready .m4a track(s) to re-acquire as MP3...\n\n", len(tracks))
	if len(tracks) == 0 {
		fmt.Println("Nothing to do.")
		return
	}

	if !execute {
		for i, t := range tracks {
			fmt.Printf("  [%d/%d] %s — %s\n      %s\n", i+1, len(tracks), t.title, t.artist, t.oldRef)
		}
		fmt.Println("\n  Run with --execute to apply (add --limit N to convert only the first N).")
		return
	}

	steps := []acqService.Step{
		acqService.NewSearchStep(searcher),
		acqService.NewSelectStep(),
		acqService.NewDownloadStep(searcher, acqService.WithDownloadProber(prober)),
		acqService.NewTagStep(),
		acqService.NewStoreStep(audioStore, acqService.WithStoreProber(prober)),
	}

	fixed, skipped := 0, 0
	for i, t := range tracks {
		if err := ctx.Err(); err != nil {
			fmt.Printf("  Cancelled: %v\n", err)
			break
		}

		album := ""
		if t.album != nil {
			album = *t.album
		}
		// reacquireAsM4a runs the pipeline and returns the new audio_ref; post-revert
		// that ref is a .mp3 (buildAudioRef changed), so this converts m4a → mp3.
		newRef, err := reacquireAsM4a(ctx, steps, audioStore, t.id, t.userId, t.title, t.artist, album, t.oldRef, t.duration)
		if err != nil {
			fmt.Printf("  [%d/%d] SKIP: %s — %s  (%v)\n", i+1, len(tracks), t.title, t.artist, err)
			skipped++
			continue
		}

		if _, err := pool.Exec(ctx,
			`UPDATE tracks SET audio_ref = $1 WHERE id = $2 AND user_id = $3`,
			newRef, t.id, t.userId); err != nil {
			fmt.Printf("  [%d/%d] SKIP (db update failed): %s — %s  (%v)\n", i+1, len(tracks), t.title, t.artist, err)
			skipped++
			continue
		}

		if newRef != t.oldRef {
			if err := audioStore.Delete(ctx, t.oldRef); err != nil {
				slog.WarnContext(ctx, "reacquire_corrupt: orphaned old m4a after db update",
					"track_id", t.id, "old_ref", t.oldRef, "error", err)
			}
		}
		fmt.Printf("  [%d/%d] OK: %s — %s  -> %s\n", i+1, len(tracks), t.title, t.artist, newRef)
		fixed++
	}

	fmt.Printf("\n%s\n", strings.Repeat("=", 50))
	fmt.Println("Re-acquire corrupt M4A complete:")
	fmt.Printf("  Candidates: %d\n", len(tracks))
	fmt.Printf("  Fixed:      %d\n", fixed)
	fmt.Printf("  Skipped:    %d  (old file kept, safe to re-run)\n", skipped)
	fmt.Println()
}
