package commands

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"altune/go-api/internal/acquisition/adapters/ytdlp"
	acqService "altune/go-api/internal/acquisition/service"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/database"
)

// perTrackTimeout bounds one track's re-acquisition (search + download + store),
// matching the acquisition service's own ceiling.
const perTrackTimeout = 10 * time.Minute

// RunBackfillM4a re-acquires every ready track whose stored audio is still the
// legacy re-encoded MP3, replacing it with the native M4A/AAC the pipeline now
// produces. Per track it re-runs the acquisition pipeline (search → select →
// download → verify → store) to the new .m4a key, then — only once the new file
// is confirmed stored — updates audio_ref and deletes the old .mp3. Ordering is
// deliberate: a re-acquire failure leaves the track's existing .mp3 and DB row
// untouched, so playback never breaks.
//
// Dry-run by default; pass execute=true to apply. limit<=0 processes all.
func RunBackfillM4a(cfg *config.Config, execute bool, limit int) {
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
		  AND audio_ref LIKE '%.mp3'
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

	fmt.Printf("\nFound %d ready MP3 track(s) to convert to M4A...\n\n", len(tracks))
	if len(tracks) == 0 {
		fmt.Println("Nothing to do.")
		return
	}

	if !execute {
		for i, t := range tracks {
			newPreview := strings.TrimSuffix(t.oldRef, ".mp3") + ".m4a"
			fmt.Printf("  [%d/%d] %s — %s\n      %s\n   -> %s\n", i+1, len(tracks), t.title, t.artist, t.oldRef, newPreview)
		}
		fmt.Println("\n  Run with --execute to apply (add --limit N to convert only the first N).")
		return
	}

	steps := []acqService.Step{
		acqService.NewSearchStep(searcher),
		acqService.NewSelectStep(),
		acqService.NewDownloadStep(searcher, acqService.WithDownloadProber(prober)),
		acqService.NewTagStep(),
		acqService.NewStoreStep(audioStore),
	}

	converted, skipped := 0, 0
	for i, t := range tracks {
		if err := ctx.Err(); err != nil {
			fmt.Printf("  Cancelled: %v\n", err)
			break
		}

		album := ""
		if t.album != nil {
			album = *t.album
		}
		newRef, err := reacquireAsM4a(ctx, steps, audioStore, t.id, t.userId, t.title, t.artist, album, t.oldRef, t.duration)
		if err != nil {
			fmt.Printf("  [%d/%d] SKIP: %s — %s  (%v)\n", i+1, len(tracks), t.title, t.artist, err)
			skipped++
			continue
		}

		if _, err := pool.Exec(ctx,
			`UPDATE tracks SET audio_ref = $1 WHERE id = $2 AND user_id = $3`,
			newRef, t.id, t.userId); err != nil {
			// The new .m4a is stored but the DB still points at the old .mp3, so the
			// track keeps playing. A later run re-processes it (idempotent), leaving
			// at worst one orphaned .m4a. Do NOT delete the old file here.
			fmt.Printf("  [%d/%d] SKIP (db update failed): %s — %s  (%v)\n", i+1, len(tracks), t.title, t.artist, err)
			skipped++
			continue
		}

		if newRef != t.oldRef {
			if err := audioStore.Delete(ctx, t.oldRef); err != nil {
				slog.WarnContext(ctx, "backfill_m4a: orphaned old mp3 after db update",
					"track_id", t.id, "old_ref", t.oldRef, "error", err)
			}
		}
		fmt.Printf("  [%d/%d] OK: %s — %s  -> %s\n", i+1, len(tracks), t.title, t.artist, newRef)
		converted++
	}

	fmt.Printf("\n%s\n", "==================================================")
	fmt.Println("Backfill M4A complete:")
	fmt.Printf("  Candidates: %d\n", len(tracks))
	fmt.Printf("  Converted:  %d\n", converted)
	fmt.Printf("  Skipped:    %d  (old MP3 kept, safe to re-run)\n", skipped)
	fmt.Println()

	slog.Info("backfill_m4a_completed",
		"candidates", len(tracks),
		"converted", converted,
		"skipped", skipped)
}

// reacquireAsM4a runs the acquisition pipeline for one track and returns the new
// .m4a audio_ref on success. It does NOT touch the database or the old file —
// the caller updates audio_ref and deletes the old .mp3 only after this returns
// cleanly. The expected duration (DB value, else probed from the existing .mp3)
// is passed so DownloadStep's prober rejects a wrong-length recording.
func reacquireAsM4a(
	ctx context.Context,
	steps []acqService.Step,
	audioStore ports.AudioStore,
	id, userId, title, artist, album, oldRef string,
	dbDuration *float64,
) (string, error) {
	perCtx, cancel := context.WithTimeout(ctx, perTrackTimeout)
	defer cancel()

	expected := 0.0
	if dbDuration != nil {
		expected = *dbDuration
	}
	if expected <= 0 {
		if d, perr := probeDuration(perCtx, audioStore, oldRef); perr == nil {
			expected = d
		}
	}

	ac := &acqService.AcquisitionContext{Track: acqService.TrackRef{
		ID:       id,
		UserID:   userId,
		Title:    title,
		Artist:   artist,
		Album:    album,
		Duration: expected,
	}}

	runErr := acqService.RunPipeline(perCtx, steps, ac)
	if ac.TempPath != "" {
		os.RemoveAll(filepath.Dir(ac.TempPath))
	}
	if runErr != nil {
		return "", runErr
	}
	if ac.AudioRef == "" {
		return "", fmt.Errorf("pipeline produced no audio ref")
	}
	return ac.AudioRef, nil
}
