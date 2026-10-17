package main

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type store struct {
	pool   *pgxpool.Pool
	access pgx.TxAccessMode
}

func (s store) begin(ctx context.Context) (pgx.Tx, error) {
	return s.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: s.access})
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
	tx, err := s.begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query tracks: %w", err)
	}
	defer rows.Close()
	var tracks []trackRow
	for rows.Next() {
		var row trackRow
		if err := rows.Scan(&row.ID, &row.UserID, &row.Artist, &row.SourceURL, &row.Provenance, &row.Verdict, &row.Version); err != nil {
			return nil, fmt.Errorf("scan track: %w", err)
		}
		tracks = append(tracks, row)
	}
	return tracks, rows.Err()
}

func (s store) applyPatch(ctx context.Context, row trackRow, p patch) (bool, error) {
	raw, err := p.json()
	if err != nil {
		return false, fmt.Errorf("encode patch: %w", err)
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, applyPatchSQL, row.ID, row.UserID, row.Version, string(raw))
	if err != nil {
		return false, fmt.Errorf("update track: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (s store) undoRun(ctx context.Context, runID uuid.UUID) (int64, error) {
	tx, err := s.begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, undoRunSQL, runID.String())
	if err != nil {
		return 0, fmt.Errorf("undo run: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return tag.RowsAffected(), nil
}
