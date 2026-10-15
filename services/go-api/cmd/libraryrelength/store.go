package main

import (
	acqports "altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const restoreTimeout = 30 * time.Second

type jobEnqueuer interface {
	Enqueue(ctx context.Context, trackID shared.TrackId, kind acqports.JobKind, delay time.Duration) error
}

type store struct {
	pool  *pgxpool.Pool
	queue jobEnqueuer
}

const selectTrackSQL = `SELECT t.isrc, t.version, t.acquisition_status, t.duration_seconds, t.catalog_duration_seconds,
	t.audio_length_mismatch, t.audio_version, t.acquisition_available_at IS NOT NULL,
	EXISTS (SELECT 1 FROM track_length_fixes f WHERE f.track_id = t.id AND f.action = 'reacquire' AND f.undone_at IS NULL)
	FROM tracks t WHERE t.id = $1 AND t.user_id = $2`

func loadTrack(ctx context.Context, tx pgx.Tx, c change, lock bool) (trackRow, error) {
	query := selectTrackSQL
	if lock {
		query += " FOR UPDATE OF t"
	}
	var row trackRow
	err := tx.QueryRow(ctx, query, c.TrackID, c.UserID).Scan(&row.ISRC, &row.Version, &row.Status, &row.DurationSeconds,
		&row.CatalogSeconds, &row.Mismatch, &row.AudioVersion, &row.InFlight, &row.PriorReacquire)
	return row, err
}

const fixISRCSQL = `UPDATE tracks SET isrc = $3, catalog_duration_seconds = $4, audio_length_mismatch = false, version = version + 1
	WHERE id = $1 AND user_id = $2 AND version = $5`

const markForReplaceSQL = `UPDATE tracks SET catalog_duration_seconds = $3, audio_length_mismatch = true, version = version + 1
	WHERE id = $1 AND user_id = $2 AND version = $4`

const insertFixSQL = `INSERT INTO track_length_fixes (run_id, track_id, user_id, action, old_isrc, new_isrc, old_duration_seconds,
	old_catalog_duration_seconds, old_audio_length_mismatch, new_catalog_duration_seconds, audio_version_at_apply)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`

func (s store) apply(ctx context.Context, runID uuid.UUID, c change, write bool) (verdict, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return verdictFailed, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	got, row, err := lockAndDecide(ctx, tx, c, write)
	if err != nil || got != verdictApplied || !write {
		return got, err
	}
	if err := writeChange(ctx, tx, runID, c, row); err != nil {
		return verdictFailed, err
	}
	if err := tx.Commit(ctx); err != nil {
		return verdictFailed, fmt.Errorf("commit: %w", err)
	}
	if c.Action == actionReacquire {
		return s.enqueueReplace(ctx, runID, c)
	}
	return verdictApplied, nil
}

func lockAndDecide(ctx context.Context, tx pgx.Tx, c change, lock bool) (verdict, trackRow, error) {
	row, err := loadTrack(ctx, tx, c, lock)
	if errors.Is(err, pgx.ErrNoRows) {
		return verdictStale, trackRow{}, nil
	}
	if err != nil {
		return verdictFailed, trackRow{}, fmt.Errorf("read track: %w", err)
	}
	return decide(c, row), row, nil
}

func writeChange(ctx context.Context, tx pgx.Tx, runID uuid.UUID, c change, row trackRow) error {
	if err := updateTrack(ctx, tx, c); err != nil {
		return err
	}
	var newISRC *string
	if c.Action == actionFixISRC {
		newISRC = &c.NewISRC
	}
	_, err := tx.Exec(ctx, insertFixSQL, runID, c.TrackID, c.UserID, string(c.Action), row.ISRC, newISRC, row.DurationSeconds,
		row.CatalogSeconds, row.Mismatch, c.CatalogSeconds, row.AudioVersion)
	if err != nil {
		return fmt.Errorf("log change: %w", err)
	}
	return nil
}

func updateTrack(ctx context.Context, tx pgx.Tx, c change) error {
	statement, args := markForReplaceSQL, []any{c.TrackID, c.UserID, c.CatalogSeconds, c.Version}
	if c.Action == actionFixISRC {
		statement, args = fixISRCSQL, []any{c.TrackID, c.UserID, c.NewISRC, c.CatalogSeconds, c.Version}
	}
	result, err := tx.Exec(ctx, statement, args...)
	if err != nil {
		return fmt.Errorf("update track: %w", err)
	}
	if result.RowsAffected() != 1 {
		return errors.New("update track: version moved under the row lock")
	}
	return nil
}

func (s store) enqueueReplace(ctx context.Context, runID uuid.UUID, c change) (verdict, error) {
	trackID, err := uuid.Parse(c.TrackID)
	if err == nil {
		err = s.queue.Enqueue(ctx, shared.TrackIdFromUUID(trackID), acqports.JobKindUpgrade, 0)
	}
	if err == nil {
		return verdictApplied, nil
	}
	return verdictEnqueueFailed, errors.Join(err, s.restoreUnqueued(ctx, runID, c.TrackID))
}

type loggedFix struct {
	TrackID             string
	UserID              string
	Action              action
	OldISRC             *string
	NewISRC             *string
	OldDurationSeconds  *float64
	OldCatalogSeconds   *float64
	OldMismatch         bool
	NewCatalogSeconds   float64
	AudioVersionAtApply *string
}

type undoVerdict string

const (
	undoRestored        undoVerdict = "restored"
	undoSkippedChanged  undoVerdict = "skipped-changed"
	undoSkippedInFlight undoVerdict = "skipped-in-flight"
	undoSkippedReplaced undoVerdict = "skipped-already-replaced"
	undoFailed          undoVerdict = "failed"
)

type undoResult struct {
	Fix     loggedFix
	Verdict undoVerdict
	Err     error
}

const loggedFixesSQL = `SELECT track_id::text, user_id::text, action, old_isrc, new_isrc, old_duration_seconds,
	old_catalog_duration_seconds, old_audio_length_mismatch, new_catalog_duration_seconds, audio_version_at_apply
	FROM track_length_fixes WHERE run_id = $1 AND ($2 = '' OR track_id::text = $2) AND undone_at IS NULL ORDER BY track_id`

func (s store) loggedFixes(ctx context.Context, runID uuid.UUID, trackID string) ([]loggedFix, error) {
	rows, err := s.pool.Query(ctx, loggedFixesSQL, runID, trackID)
	if err != nil {
		return nil, fmt.Errorf("query change log: %w", err)
	}
	defer rows.Close()
	var fixes []loggedFix
	for rows.Next() {
		var f loggedFix
		var rawAction string
		if err := rows.Scan(&f.TrackID, &f.UserID, &rawAction, &f.OldISRC, &f.NewISRC, &f.OldDurationSeconds,
			&f.OldCatalogSeconds, &f.OldMismatch, &f.NewCatalogSeconds, &f.AudioVersionAtApply); err != nil {
			return nil, fmt.Errorf("scan change log: %w", err)
		}
		f.Action = action(rawAction)
		fixes = append(fixes, f)
	}
	return fixes, rows.Err()
}

var errNothingToUndo = errors.New("nothing to undo")

func (s store) undoRun(ctx context.Context, runID uuid.UUID) ([]undoResult, error) {
	fixes, err := s.loggedFixes(ctx, runID, "")
	if err != nil {
		return nil, err
	}
	if len(fixes) == 0 {
		return nil, fmt.Errorf("%w for run_id %s", errNothingToUndo, runID)
	}
	results := make([]undoResult, 0, len(fixes))
	for _, f := range fixes {
		verdict, err := s.undoRow(ctx, runID, f)
		results = append(results, undoResult{Fix: f, Verdict: verdict, Err: err})
	}
	return results, nil
}

const currentForUndoSQL = `SELECT isrc, catalog_duration_seconds, audio_version, acquisition_available_at IS NOT NULL
	FROM tracks WHERE id = $1 AND user_id = $2 FOR UPDATE`

const restoreFixSQL = `UPDATE tracks SET isrc = $3, catalog_duration_seconds = $4, audio_length_mismatch = $5, version = version + 1
	WHERE id = $1 AND user_id = $2`

const restoreReplaceSQL = `UPDATE tracks SET duration_seconds = $3, catalog_duration_seconds = $4, audio_length_mismatch = $5, version = version + 1
	WHERE id = $1 AND user_id = $2`

const stampUndoneSQL = `UPDATE track_length_fixes SET undone_at = now() WHERE run_id = $1 AND track_id = $2`

func (s store) undoRow(ctx context.Context, runID uuid.UUID, f loggedFix) (undoVerdict, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return undoFailed, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	verdict, err := checkUndoable(ctx, tx, f)
	if verdict != undoRestored {
		return verdict, err
	}
	if err := restoreAndStamp(ctx, tx, runID, f); err != nil {
		return undoFailed, err
	}
	if err := tx.Commit(ctx); err != nil {
		return undoFailed, fmt.Errorf("commit: %w", err)
	}
	return undoRestored, nil
}

func checkUndoable(ctx context.Context, tx pgx.Tx, f loggedFix) (undoVerdict, error) {
	var isrc, audioVersion *string
	var catalog *float64
	var inFlight bool
	err := tx.QueryRow(ctx, currentForUndoSQL, f.TrackID, f.UserID).Scan(&isrc, &catalog, &audioVersion, &inFlight)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return undoSkippedChanged, nil
	case err != nil:
		return undoFailed, fmt.Errorf("read track: %w", err)
	case catalog == nil || *catalog != f.NewCatalogSeconds:
		return undoSkippedChanged, nil
	case f.Action == actionFixISRC && !sameText(isrc, f.NewISRC):
		return undoSkippedChanged, nil
	case f.Action == actionReacquire && inFlight:
		return undoSkippedInFlight, nil
	case f.Action == actionReacquire && !sameText(audioVersion, f.AudioVersionAtApply):
		return undoSkippedReplaced, nil
	}
	return undoRestored, nil
}

func sameText(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func restoreAndStamp(ctx context.Context, tx pgx.Tx, runID uuid.UUID, f loggedFix) error {
	var err error
	if f.Action == actionFixISRC {
		_, err = tx.Exec(ctx, restoreFixSQL, f.TrackID, f.UserID, f.OldISRC, f.OldCatalogSeconds, f.OldMismatch)
	} else {
		_, err = tx.Exec(ctx, restoreReplaceSQL, f.TrackID, f.UserID, f.OldDurationSeconds, f.OldCatalogSeconds, f.OldMismatch)
	}
	if err != nil {
		return fmt.Errorf("restore track: %w", err)
	}
	if _, err := tx.Exec(ctx, stampUndoneSQL, runID, f.TrackID); err != nil {
		return fmt.Errorf("stamp undone_at: %w", err)
	}
	return nil
}

func (s store) restoreUnqueued(ctx context.Context, runID uuid.UUID, trackID string) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), restoreTimeout)
	defer cancel()
	fixes, err := s.loggedFixes(ctx, runID, trackID)
	if err != nil || len(fixes) != 1 {
		return errors.Join(err, fmt.Errorf("restoring track %s: found %d logged changes", trackID, len(fixes)))
	}
	verdict, err := s.undoRow(ctx, runID, fixes[0])
	if verdict != undoRestored {
		return fmt.Errorf("restoring track %s: %s: %w", trackID, verdict, err)
	}
	return nil
}
