package main

import (
	"altune/go-api/internal/acquisition/ports"
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const provider = string(ports.ProviderYouTube)

type store struct {
	pool   *pgxpool.Pool
	access pgx.TxAccessMode
}

func (s store) begin(ctx context.Context) (pgx.Tx, error) {
	return s.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: s.access})
}

type writeVerdict string

const (
	writeDone    writeVerdict = "written"
	writeSkipped writeVerdict = "skipped"
)

const targetsSQL = `SELECT t.id::text, t.user_id::text, t.artist, t.title, COALESCE(t.album, ''), COALESCE(i.isrc, t.isrc, ''),
	COALESCE(t.catalog_duration_seconds, 0), t.version
	FROM tracks t
	JOIN track_identity i ON i.track_id = t.id
	WHERE t.acquisition_status = 'ready'
	  AND i.status = 'bound'
	  AND NOT EXISTS (SELECT 1 FROM jsonb_array_elements(t.catalog_sources) s WHERE s->>'provider' = $2::text)
	  AND ($1 = '' OR t.user_id::text = $1)
	ORDER BY t.user_id, t.artist, t.title, t.id`

func (s store) loadTargets(ctx context.Context, userID string) ([]target, error) {
	tx, err := s.begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, targetsSQL, userID, provider)
	if err != nil {
		return nil, fmt.Errorf("query bound tracks without an official pointer: %w", err)
	}
	defer rows.Close()
	var targets []target
	for rows.Next() {
		var t target
		if err := rows.Scan(&t.TrackID, &t.UserID, &t.Artist, &t.Title, &t.Album, &t.ISRC, &t.CatalogSeconds, &t.Version); err != nil {
			return nil, fmt.Errorf("scan bound track: %w", err)
		}
		targets = append(targets, t)
	}
	return targets, rows.Err()
}

const pointSQL = `WITH prior AS (
	SELECT catalog_sources FROM tracks
	WHERE id = $1 AND version = $2
	  AND NOT EXISTS (SELECT 1 FROM jsonb_array_elements(catalog_sources) s WHERE s->>'provider' = $3::text)
	FOR UPDATE
) UPDATE tracks t
	SET catalog_sources = prior.catalog_sources || jsonb_build_array(jsonb_build_object('provider', $3::text, 'external_id', $4::text))
	FROM prior WHERE t.id = $1
	RETURNING prior.catalog_sources::text, t.catalog_sources::text`

const logPointerSQL = `INSERT INTO track_metadata_changes (run_id, track_id, user_id, field, old_value, new_value)
	VALUES ($1, $2, $3, 'catalog_sources', $4, $5)`

func (s store) point(ctx context.Context, runID uuid.UUID, t target, videoID string) (writeVerdict, error) {
	tx, err := s.begin(ctx)
	if err != nil {
		return writeSkipped, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var before, after string
	err = tx.QueryRow(ctx, pointSQL, t.TrackID, t.Version, provider, videoID).Scan(&before, &after)
	if errors.Is(err, pgx.ErrNoRows) {
		return writeSkipped, nil
	}
	if err != nil {
		return writeSkipped, fmt.Errorf("point catalog_sources: %w", err)
	}
	if _, err := tx.Exec(ctx, logPointerSQL, runID, t.TrackID, t.UserID, before, after); err != nil {
		return writeSkipped, fmt.Errorf("log pointer: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return writeSkipped, fmt.Errorf("commit: %w", err)
	}
	return writeDone, nil
}

type undoTally struct {
	Restored int
	Kept     int
}

var errNothingToUndo = errors.New("nothing to undo")

const undoSQL = `WITH restored AS (
	UPDATE tracks t SET catalog_sources = c.old_value::jsonb FROM track_metadata_changes c
	WHERE c.run_id = $1 AND c.field = 'catalog_sources' AND c.undone_at IS NULL
	  AND t.id = c.track_id AND t.catalog_sources = c.new_value::jsonb
	RETURNING c.track_id
), stamped AS (
	UPDATE track_metadata_changes SET undone_at = now()
	WHERE run_id = $1 AND field = 'catalog_sources' AND undone_at IS NULL AND track_id IN (SELECT track_id FROM restored)
	RETURNING track_id
) SELECT (SELECT count(*) FROM restored),
	(SELECT count(*) FROM track_metadata_changes WHERE run_id = $1 AND field = 'catalog_sources' AND undone_at IS NULL) - (SELECT count(*) FROM restored)`

func (s store) undo(ctx context.Context, runID uuid.UUID) (undoTally, error) {
	tx, err := s.begin(ctx)
	if err != nil {
		return undoTally{}, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var tally undoTally
	if err := tx.QueryRow(ctx, undoSQL, runID).Scan(&tally.Restored, &tally.Kept); err != nil {
		return undoTally{}, fmt.Errorf("undo run: %w", err)
	}
	if tally.Restored+tally.Kept == 0 {
		return tally, fmt.Errorf("%w for run_id %s", errNothingToUndo, runID)
	}
	if err := tx.Commit(ctx); err != nil {
		return undoTally{}, fmt.Errorf("commit: %w", err)
	}
	return tally, nil
}
