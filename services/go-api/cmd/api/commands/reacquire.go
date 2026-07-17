package commands

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"altune/go-api/internal/acquisition/adapters/id3"
	"altune/go-api/internal/acquisition/adapters/ytdlp"
	acqService "altune/go-api/internal/acquisition/service"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/database"
)

// perTrackTimeout bounds one track's re-acquisition (search + download + store),
// matching the acquisition service's own ceiling.
const perTrackTimeout = 10 * time.Minute

// reacquireSpec is what actually differs between the re-acquisition commands:
// which stored refs to select and how to talk about them. The loop — query rows,
// dry-run print, run pipeline, swap audio_ref, delete the old file — is shared.
type reacquireSpec struct {
	refLike           string // SQL LIKE pattern selecting the audio_refs to re-acquire
	banner            string // Printf format with one %d (candidate count)
	doneHeading       string
	okLabel           string // summary label for successful tracks
	orphanLogMsg      string
	completedLogEvent string // slog event emitted on completion; "" → none
}

// runReacquire re-acquires every ready track whose audio_ref matches the spec,
// through the fully-gated acquisition pipeline (search → select → download →
// verify → tag → store). Per track: store the new file → swap audio_ref → delete
// the old one, so a failed or gate-rejected re-acquire leaves the existing file
// and DB row untouched. Dry-run by default; execute=true applies; limit<=0
// processes all. Safe to re-run.
func runReacquire(cfg *config.Config, execute bool, limit int, spec reacquireSpec) {
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
		  AND audio_ref LIKE '`+spec.refLike+`'
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

	fmt.Printf("\n"+spec.banner+"\n\n", len(tracks))
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
		acqService.NewTagStep(id3.NewTagger()),
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
		newRef, err := reacquireTrack(ctx, steps, audioStore, t.id, t.userId, t.title, t.artist, album, t.oldRef, t.duration)
		if err != nil {
			fmt.Printf("  [%d/%d] SKIP: %s — %s  (%v)\n", i+1, len(tracks), t.title, t.artist, err)
			skipped++
			continue
		}

		if _, err := pool.Exec(ctx,
			`UPDATE tracks SET audio_ref = $1 WHERE id = $2 AND user_id = $3`,
			newRef, t.id, t.userId); err != nil {
			// The new file is stored but the DB still points at the old ref, so the
			// track keeps playing. A later run re-processes it (idempotent), leaving
			// at worst one orphaned file. Do NOT delete the old file here.
			fmt.Printf("  [%d/%d] SKIP (db update failed): %s — %s  (%v)\n", i+1, len(tracks), t.title, t.artist, err)
			skipped++
			continue
		}

		if newRef != t.oldRef {
			if err := audioStore.Delete(ctx, t.oldRef); err != nil {
				slog.WarnContext(ctx, spec.orphanLogMsg,
					"track_id", t.id, "old_ref", t.oldRef, "error", err)
			}
		}
		fmt.Printf("  [%d/%d] OK: %s — %s  -> %s\n", i+1, len(tracks), t.title, t.artist, newRef)
		fixed++
	}

	fmt.Printf("\n%s\n", strings.Repeat("=", 50))
	fmt.Println(spec.doneHeading)
	fmt.Printf("  Candidates: %d\n", len(tracks))
	fmt.Printf("  %-11s %d\n", spec.okLabel+":", fixed)
	fmt.Printf("  Skipped:    %d  (old file kept, safe to re-run)\n", skipped)
	fmt.Println()

	if spec.completedLogEvent != "" {
		slog.Info(spec.completedLogEvent,
			"candidates", len(tracks),
			"converted", fixed,
			"skipped", skipped)
	}
}

// reacquireTrack runs the acquisition pipeline for one track and returns the new
// audio_ref on success (the pipeline derives its extension from the file the
// download actually produced). It does NOT touch the database or the old file —
// the caller updates audio_ref and deletes the old one only after this returns
// cleanly. The expected duration (DB value, else probed from the existing file)
// is passed so DownloadStep's prober rejects a wrong-length recording.
func reacquireTrack(
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
