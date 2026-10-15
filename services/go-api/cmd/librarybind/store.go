package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type store struct{ pool *pgxpool.Pool }

type saveVerdict string

const (
	saveWritten saveVerdict = "written"
	saveSkipped saveVerdict = "skipped"
)

const unidentifiedTracksSQL = `SELECT t.id::text, t.user_id::text, t.title, t.artist, COALESCE(t.album, ''), COALESCE(t.isrc, ''),
	COALESCE(t.duration_seconds, 0), t.version, COALESCE(t.audio_source_url, '')
	FROM tracks t
	LEFT JOIN track_identity i ON i.track_id = t.id
	WHERE t.acquisition_status = 'ready'
	  AND (i.track_id IS NULL OR i.status = 'unbound')
	  AND ($1 = '' OR t.user_id::text = $1)
	ORDER BY t.user_id, t.artist, t.album, t.title
	LIMIT NULLIF($2, 0)`

func (s store) loadTracks(ctx context.Context, userID string, limit int) ([]libraryTrack, error) {
	rows, err := s.pool.Query(ctx, unidentifiedTracksSQL, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("query ready tracks: %w", err)
	}
	defer rows.Close()
	var tracks []libraryTrack
	for rows.Next() {
		var t libraryTrack
		if err := rows.Scan(&t.ID, &t.UserID, &t.Title, &t.Artist, &t.Album, &t.ISRC, &t.FileSeconds, &t.Version, &t.SourceURL); err != nil {
			return nil, fmt.Errorf("scan ready track: %w", err)
		}
		tracks = append(tracks, t)
	}
	return tracks, rows.Err()
}

const pinTargetsSQL = `SELECT t.id::text, t.user_id::text, t.title, t.artist, COALESCE(t.album, ''), COALESCE(t.isrc, ''),
	COALESCE(t.duration_seconds, 0), t.version, COALESCE(i.status, '')
	FROM tracks t
	LEFT JOIN track_identity i ON i.track_id = t.id
	WHERE t.id::text = ANY($1::text[])`

func (s store) loadPinTargets(ctx context.Context, trackIDs []string) (map[string]pinTarget, error) {
	rows, err := s.pool.Query(ctx, pinTargetsSQL, trackIDs)
	if err != nil {
		return nil, fmt.Errorf("query pinned tracks: %w", err)
	}
	defer rows.Close()
	targets := map[string]pinTarget{}
	for rows.Next() {
		var target pinTarget
		t := &target.Track
		if err := rows.Scan(&t.ID, &t.UserID, &t.Title, &t.Artist, &t.Album, &t.ISRC, &t.FileSeconds, &t.Version, &target.IdentityStatus); err != nil {
			return nil, fmt.Errorf("scan pinned track: %w", err)
		}
		targets[t.ID] = target
	}
	return targets, rows.Err()
}

const saveIdentitySQL = `INSERT INTO track_identity (track_id, user_id, isrc, mb_recording_id, deezer_track_id, apple_song_id,
	status, reason, evidence, run_id, track_version_at_bind)
	SELECT t.id, t.user_id, $3::text, $4::text, $5::text, $6::text, $7::text, $8::text, $9::jsonb, $10::uuid, t.version
	FROM tracks t WHERE t.id = $1 AND t.version = $2
	ON CONFLICT (track_id) DO UPDATE SET isrc = EXCLUDED.isrc, mb_recording_id = EXCLUDED.mb_recording_id,
		deezer_track_id = EXCLUDED.deezer_track_id, apple_song_id = EXCLUDED.apple_song_id, status = EXCLUDED.status,
		reason = EXCLUDED.reason, evidence = EXCLUDED.evidence, run_id = EXCLUDED.run_id, bound_at = now(),
		track_version_at_bind = EXCLUDED.track_version_at_bind
	WHERE track_identity.status = 'unbound'`

func (s store) save(ctx context.Context, runID uuid.UUID, t libraryTrack, found identity) (saveVerdict, error) {
	proof, err := json.Marshal(found.Evidence)
	if err != nil {
		return saveSkipped, fmt.Errorf("encode evidence: %w", err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return saveSkipped, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	result, err := tx.Exec(ctx, saveIdentitySQL, t.ID, t.Version, nullable(found.ISRC), nullable(found.MBRecordingID),
		nullable(found.DeezerTrackID), nullable(found.AppleSongID), string(found.Status), found.Reason, proof, runID)
	if err != nil {
		return saveSkipped, fmt.Errorf("save identity: %w", err)
	}
	if result.RowsAffected() == 0 {
		return saveSkipped, nil
	}
	if err := pinBoundRecording(ctx, tx, runID, t, found); err != nil {
		return saveSkipped, err
	}
	if err := tx.Commit(ctx); err != nil {
		return saveSkipped, fmt.Errorf("commit: %w", err)
	}
	return saveWritten, nil
}

func pinBoundRecording(ctx context.Context, tx pgx.Tx, runID uuid.UUID, t libraryTrack, found identity) error {
	if found.Status != statusBound || !isPinnableRecording(found.MBRecordingID) {
		return nil
	}
	_, err := catalogPinInTx(ctx, tx, runID, catalogPinCandidate{TrackID: t.ID, UserID: t.UserID, RecordingID: strings.ToLower(found.MBRecordingID), Version: t.Version})
	return err
}

func nullable(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

type undoTally struct {
	Removed  int
	Kept     int
	Unpinned int
	PinsKept int
}

func (u undoTally) isEmpty() bool {
	return u.Removed+u.Kept+u.Unpinned+u.PinsKept == 0
}

var errNothingToUndo = errors.New("nothing to undo")

const undoRunSQL = `WITH removed AS (
	DELETE FROM track_identity i USING tracks t
	WHERE i.run_id = $1 AND t.id = i.track_id AND t.version = i.track_version_at_bind
	RETURNING i.track_id
) SELECT (SELECT count(*) FROM removed), (SELECT count(*) FROM track_identity WHERE run_id = $1) - (SELECT count(*) FROM removed)`

const undoPinsSQL = `WITH unpinned AS (
	UPDATE tracks t SET catalog_mbid = NULL FROM track_metadata_changes c
	WHERE c.run_id = $1 AND c.field = 'catalog_mbid' AND c.undone_at IS NULL
	  AND t.id = c.track_id AND t.catalog_mbid = c.new_value
	RETURNING c.track_id
), stamped AS (
	UPDATE track_metadata_changes SET undone_at = now()
	WHERE run_id = $1 AND field = 'catalog_mbid' AND undone_at IS NULL AND track_id IN (SELECT track_id FROM unpinned)
	RETURNING track_id
) SELECT (SELECT count(*) FROM unpinned),
	(SELECT count(*) FROM track_metadata_changes WHERE run_id = $1 AND field = 'catalog_mbid' AND undone_at IS NULL) - (SELECT count(*) FROM unpinned)`

func (s store) undoRun(ctx context.Context, runID uuid.UUID) (undoTally, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return undoTally{}, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var tally undoTally
	if err := tx.QueryRow(ctx, undoRunSQL, runID).Scan(&tally.Removed, &tally.Kept); err != nil {
		return undoTally{}, fmt.Errorf("undo run: %w", err)
	}
	if err := tx.QueryRow(ctx, undoPinsSQL, runID).Scan(&tally.Unpinned, &tally.PinsKept); err != nil {
		return undoTally{}, fmt.Errorf("undo pins: %w", err)
	}
	if tally.isEmpty() {
		return tally, fmt.Errorf("%w for run_id %s", errNothingToUndo, runID)
	}
	if err := tx.Commit(ctx); err != nil {
		return undoTally{}, fmt.Errorf("commit: %w", err)
	}
	return tally, nil
}
