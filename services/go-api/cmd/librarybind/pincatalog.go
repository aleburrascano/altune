package main

import (
	"altune/go-api/internal/catalog/domain"
	"context"
	"fmt"
	"io"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type catalogPinCandidate struct {
	TrackID     string
	UserID      string
	Artist      string
	Title       string
	RecordingID string
	Version     int
	Current     *string
}

type catalogPinVerdict string

const (
	catalogPinWrite    catalogPinVerdict = "write"
	catalogPinConflict catalogPinVerdict = "conflict"
	catalogPinInvalid  catalogPinVerdict = "invalid-recording-id"
)

func (c catalogPinCandidate) verdict() catalogPinVerdict {
	switch {
	case !isPinnableRecording(c.RecordingID):
		return catalogPinInvalid
	case c.Current != nil:
		return catalogPinConflict
	}
	return catalogPinWrite
}

func isPinnableRecording(recordingID string) bool {
	_, err := domain.NormalizeCatalogMBID(recordingID)
	return err == nil
}

const catalogPinCandidatesSQL = `SELECT t.id::text, t.user_id::text, t.artist, t.title, lower(i.mb_recording_id), t.version, t.catalog_mbid
	FROM tracks t
	JOIN track_identity i ON i.track_id = t.id
	WHERE t.acquisition_status = 'ready'
	  AND i.status = 'bound'
	  AND i.mb_recording_id IS NOT NULL
	  AND t.catalog_mbid IS DISTINCT FROM lower(i.mb_recording_id)
	  AND ($1 = '' OR t.user_id::text = $1)
	ORDER BY t.user_id, t.artist, t.title, t.id`

func (s store) loadCatalogPinCandidates(ctx context.Context, userID string) ([]catalogPinCandidate, error) {
	rows, err := s.pool.Query(ctx, catalogPinCandidatesSQL, userID)
	if err != nil {
		return nil, fmt.Errorf("query bound tracks to pin: %w", err)
	}
	defer rows.Close()
	var candidates []catalogPinCandidate
	for rows.Next() {
		var c catalogPinCandidate
		if err := rows.Scan(&c.TrackID, &c.UserID, &c.Artist, &c.Title, &c.RecordingID, &c.Version, &c.Current); err != nil {
			return nil, fmt.Errorf("scan bound track to pin: %w", err)
		}
		candidates = append(candidates, c)
	}
	return candidates, rows.Err()
}

const catalogPinSQL = `UPDATE tracks SET catalog_mbid = $3 WHERE id = $1 AND version = $2 AND catalog_mbid IS NULL`

const logCatalogPinSQL = `INSERT INTO track_metadata_changes (run_id, track_id, user_id, field, old_value, new_value)
	VALUES ($1, $2, $3, 'catalog_mbid', NULL, $4)`

func catalogPinInTx(ctx context.Context, tx pgx.Tx, runID uuid.UUID, c catalogPinCandidate) (saveVerdict, error) {
	result, err := tx.Exec(ctx, catalogPinSQL, c.TrackID, c.Version, c.RecordingID)
	if err != nil {
		return saveSkipped, fmt.Errorf("pin catalog_mbid: %w", err)
	}
	if result.RowsAffected() == 0 {
		return saveSkipped, nil
	}
	if _, err := tx.Exec(ctx, logCatalogPinSQL, runID, c.TrackID, c.UserID, c.RecordingID); err != nil {
		return saveSkipped, fmt.Errorf("log pin: %w", err)
	}
	return saveWritten, nil
}

func (s store) writeCatalogPin(ctx context.Context, runID uuid.UUID, c catalogPinCandidate) (saveVerdict, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return saveSkipped, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	verdict, err := catalogPinInTx(ctx, tx, runID, c)
	if err != nil || verdict == saveSkipped {
		return verdict, err
	}
	if err := tx.Commit(ctx); err != nil {
		return saveSkipped, fmt.Errorf("commit: %w", err)
	}
	return saveWritten, nil
}

type catalogPinTally struct {
	ToPin     []catalogPinCandidate
	Conflicts []catalogPinCandidate
	Invalid   []catalogPinCandidate
	Written   int
	Skipped   int
}

func sortCatalogPins(candidates []catalogPinCandidate) catalogPinTally {
	var sorted catalogPinTally
	for _, c := range candidates {
		switch c.verdict() {
		case catalogPinInvalid:
			sorted.Invalid = append(sorted.Invalid, c)
		case catalogPinConflict:
			sorted.Conflicts = append(sorted.Conflicts, c)
		case catalogPinWrite:
			sorted.ToPin = append(sorted.ToPin, c)
		}
	}
	return sorted
}

func runCatalogPin(ctx context.Context, s store, opts options, out io.Writer) error {
	candidates, err := s.loadCatalogPinCandidates(ctx, opts.userID)
	if err != nil {
		return err
	}
	done := sortCatalogPins(candidates)
	if opts.limit > 0 {
		done.ToPin = done.ToPin[:min(len(done.ToPin), opts.limit)]
	}
	runID := uuid.New()
	var runErr error
	if opts.apply {
		runErr = applyCatalogPins(ctx, s, runID, &done)
	}
	writePinCatalogReport(out, done, opts.apply, runID)
	return runErr
}

func applyCatalogPins(ctx context.Context, s store, runID uuid.UUID, done *catalogPinTally) error {
	for _, c := range done.ToPin {
		verdict, err := s.writeCatalogPin(ctx, runID, c)
		if err != nil {
			return fmt.Errorf("aborted after %d of %d pins: %w", done.Written+done.Skipped, len(done.ToPin), err)
		}
		if verdict == saveWritten {
			done.Written++
			continue
		}
		done.Skipped++
	}
	return nil
}

func writePinCatalogReport(out io.Writer, done catalogPinTally, apply bool, runID uuid.UUID) {
	mode := "dry run, nothing written"
	if apply {
		mode = "applied"
	}
	_, _ = fmt.Fprintf(out, "run %s (%s): pin catalog_mbid\n", runID, mode)
	_, _ = fmt.Fprintf(out, "  to pin     %d\n", len(done.ToPin))
	_, _ = fmt.Fprintf(out, "  conflict   %d (catalog_mbid already set to another recording, left alone)\n", len(done.Conflicts))
	_, _ = fmt.Fprintf(out, "  invalid    %d (bound recording id is not a UUID)\n", len(done.Invalid))
	if apply {
		_, _ = fmt.Fprintf(out, "  written    %d\n  skipped    %d (track changed since it was read)\n", done.Written, done.Skipped)
	}
	_, _ = fmt.Fprintln(out, "sample:")
	for _, group := range []struct {
		label      string
		candidates []catalogPinCandidate
	}{{"pin", done.ToPin}, {"conflict", done.Conflicts}, {"invalid", done.Invalid}} {
		for _, c := range group.candidates[:min(len(group.candidates), samplesPerStatus)] {
			_, _ = fmt.Fprintf(out, "  %-8s %s - %s (%s): %s\n", group.label, c.Artist, c.Title, c.TrackID, catalogPinSampleDetail(c))
		}
	}
}

func catalogPinSampleDetail(c catalogPinCandidate) string {
	if c.Current == nil {
		return c.RecordingID
	}
	return fmt.Sprintf("has %s, bound to %s", *c.Current, c.RecordingID)
}
