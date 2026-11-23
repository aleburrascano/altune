package main

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type store struct {
	pool  *pgxpool.Pool
	runTx func(context.Context, *pgxpool.Pool, func(pgx.Tx) error) error
}

const ungradedTracksSQL = `SELECT id::text, user_id::text, artist, COALESCE(audio_source_url, ''), COALESCE(acquisition_provenance, ''),
	COALESCE(acquisition_evidence->>'verdict', ''), version
	FROM tracks
	WHERE acquisition_status = 'ready' AND audio_ref IS NOT NULL AND acquisition_evidence->>'source_kind' IS NULL
	AND ($1 = '' OR user_id::text = $1)
	ORDER BY id`

const applyPatchSQL = `UPDATE tracks
	SET acquisition_evidence = COALESCE(acquisition_evidence, '{}'::jsonb) || $4::jsonb, version = version + 1
	WHERE id = $1 AND user_id = $2 AND version = $3 AND acquisition_evidence->>'source_kind' IS NULL`

const undoRunSQL = `UPDATE tracks
	SET acquisition_evidence = NULLIF(acquisition_evidence - 'source_kind' - 'pipeline_generation' - 'grade' - 'backfill_run', '{}'::jsonb),
		version = version + 1
	WHERE acquisition_evidence->>'backfill_run' = $1`

func (s store) ungradedTracks(ctx context.Context, userID string, limit int) ([]trackRow, error) {
	query := ungradedTracksSQL
	args := []any{userID}
	if limit > 0 {
		query += " LIMIT $2"
		args = append(args, limit)
	}
	var tracks []trackRow
	err := s.runTx(ctx, s.pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, query, args...)
		if err != nil {
			return fmt.Errorf("query tracks: %w", err)
		}
		tracks, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (trackRow, error) {
			var row trackRow
			err := r.Scan(&row.ID, &row.UserID, &row.Artist, &row.SourceURL, &row.Provenance, &row.Verdict, &row.Version)
			return row, err
		})
		if err != nil {
			return fmt.Errorf("scan tracks: %w", err)
		}
		return nil
	})
	return tracks, err
}

func (s store) applyPatch(ctx context.Context, row trackRow, p patch) (bool, error) {
	raw, err := p.json()
	if err != nil {
		return false, fmt.Errorf("encode patch: %w", err)
	}
	var patched bool
	err = s.runTx(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, applyPatchSQL, row.ID, row.UserID, row.Version, string(raw))
		if err != nil {
			return fmt.Errorf("update track: %w", err)
		}
		patched = tag.RowsAffected() == 1
		return nil
	})
	if err != nil {
		return false, err
	}
	return patched, nil
}

func (s store) undoRun(ctx context.Context, runID uuid.UUID) (int64, error) {
	var undone int64
	err := s.runTx(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, undoRunSQL, runID.String())
		if err != nil {
			return fmt.Errorf("undo run: %w", err)
		}
		undone = tag.RowsAffected()
		return nil
	})
	if err != nil {
		return 0, err
	}
	return undone, nil
}
