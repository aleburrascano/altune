package main

import (
	"context"
	"fmt"
	"io"
	"math"

	"github.com/jackc/pgx/v5/pgxpool"
)

type outcome string

const (
	outcomeQueued      outcome = "queued"
	outcomeFits        outcome = "ready, length fits"
	outcomeStillOff    outcome = "replaced, still off"
	outcomeNotReplaced outcome = "not replaced"
)

type statusRow struct {
	TrackID               string
	Artist                string
	Title                 string
	InFlight              bool
	AudioVersionAtApply   *string
	AudioVersion          *string
	Mismatch              bool
	OldFileSeconds        *float64
	FileSeconds           *float64
	CatalogSeconds        float64
	PlannedCatalogSeconds float64
}

func (r statusRow) replaced() bool {
	return !sameText(r.AudioVersion, r.AudioVersionAtApply)
}

func (r statusRow) outcome() outcome {
	switch {
	case r.InFlight:
		return outcomeQueued
	case !r.replaced():
		return outcomeNotReplaced
	case r.Mismatch:
		return outcomeStillOff
	}
	return outcomeFits
}

func (r statusRow) wasWrongAudio() bool {
	return r.OldFileSeconds != nil && math.Abs(*r.OldFileSeconds-r.PlannedCatalogSeconds) <= fitToleranceSeconds
}

func (r statusRow) listenFileSeconds() float64 {
	source := r.FileSeconds
	if !r.replaced() {
		source = r.OldFileSeconds
	}
	if source == nil {
		return math.NaN()
	}
	return *source
}

const statusSQL = `SELECT DISTINCT ON (f.track_id) f.track_id::text, t.artist, t.title, t.acquisition_available_at IS NOT NULL,
	f.audio_version_at_apply, t.audio_version, t.audio_length_mismatch, f.old_duration_seconds, t.duration_seconds,
	COALESCE(t.catalog_duration_seconds, f.new_catalog_duration_seconds), f.new_catalog_duration_seconds
	FROM track_length_fixes f JOIN tracks t ON t.id = f.track_id AND t.user_id = f.user_id
	WHERE f.action = 'reacquire' AND f.undone_at IS NULL
	ORDER BY f.track_id, f.applied_at DESC`

func loadStatus(ctx context.Context, pool *pgxpool.Pool) ([]statusRow, error) {
	rows, err := pool.Query(ctx, statusSQL)
	if err != nil {
		return nil, fmt.Errorf("query status: %w", err)
	}
	defer rows.Close()
	var out []statusRow
	for rows.Next() {
		var r statusRow
		if err := rows.Scan(&r.TrackID, &r.Artist, &r.Title, &r.InFlight, &r.AudioVersionAtApply, &r.AudioVersion,
			&r.Mismatch, &r.OldFileSeconds, &r.FileSeconds, &r.CatalogSeconds, &r.PlannedCatalogSeconds); err != nil {
			return nil, fmt.Errorf("scan status: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func writeStatus(out io.Writer, rows []statusRow) {
	counts := map[outcome]int{}
	var listen, wrongAudio []statusRow
	for _, r := range rows {
		o := r.outcome()
		counts[o]++
		_, _ = fmt.Fprintf(out, "%-20s %s - %s%s\n", o, r.Artist, r.Title, wrongAudioMarker(r))
		if r.wasWrongAudio() {
			wrongAudio = append(wrongAudio, r)
		}
		if o == outcomeStillOff || o == outcomeNotReplaced {
			listen = append(listen, r)
		}
	}
	_, _ = fmt.Fprintf(out, "\n%d re-acquired tracks\n  queued:              %d\n  ready, length fits:  %d\n  replaced, still off: %d\n  not replaced:        %d\n",
		len(rows), counts[outcomeQueued], counts[outcomeFits], counts[outcomeStillOff], counts[outcomeNotReplaced])
	writeListenList(out, listen)
	writeVerifyNote(out, wrongAudio)
}

func wrongAudioMarker(r statusRow) string {
	if r.wasWrongAudio() {
		return " [wrong audio]"
	}
	return ""
}

func writeVerifyNote(out io.Writer, wrongAudio []statusRow) {
	if len(wrongAudio) == 0 {
		return
	}
	_, _ = fmt.Fprintf(out, "\nwrong-audio re-acquires (%d): once they finish, run libraryverify on them to confirm the new audio is the right song\n", len(wrongAudio))
	for _, r := range wrongAudio {
		_, _ = fmt.Fprintf(out, "  %s - %s (%s)\n", r.Artist, r.Title, r.outcome())
	}
}

func writeListenList(out io.Writer, listen []statusRow) {
	_, _ = fmt.Fprintf(out, "\nlisten list (%d): file length vs catalog length\n", len(listen))
	for _, r := range listen {
		_, _ = fmt.Fprintf(out, "  %s - %s: %s vs %ds (%s)\n", r.Artist, r.Title, seconds(r.listenFileSeconds()), int(r.CatalogSeconds), r.outcome())
	}
}

func seconds(value float64) string {
	if math.IsNaN(value) {
		return "unknown"
	}
	return fmt.Sprintf("%ds", int(value))
}
