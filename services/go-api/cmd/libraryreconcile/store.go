package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var errNothingToUndo = errors.New("nothing to undo")

type decision struct {
	Change  plannedChange
	Verdict verdict
	Current *string
}

type store struct {
	pool *pgxpool.Pool
}

type trackRow struct {
	Values  map[field]*string
	Version int
}

const selectTrackSQL = `SELECT year::text, album_artist, isrc, version FROM tracks WHERE id = $1 AND user_id = $2`

func loadTrack(ctx context.Context, tx pgx.Tx, plan trackPlan, lock bool) (trackRow, error) {
	query := selectTrackSQL
	if lock {
		query += " FOR UPDATE"
	}
	var year, albumArtist, isrc *string
	var version int
	if err := tx.QueryRow(ctx, query, plan.TrackID, plan.UserID).Scan(&year, &albumArtist, &isrc, &version); err != nil {
		return trackRow{}, err
	}
	return trackRow{
		Values:  map[field]*string{fieldYear: year, fieldAlbumArtist: albumArtist, fieldISRC: isrc},
		Version: version,
	}, nil
}

func decideTrack(plan trackPlan, row trackRow) []decision {
	decisions := make([]decision, 0, len(plan.Changes))
	for _, change := range plan.Changes {
		current := row.Values[change.Field]
		decisions = append(decisions, decision{Change: change, Verdict: decide(change, current), Current: current})
	}
	return decisions
}

func allStale(plan trackPlan) []decision {
	decisions := make([]decision, 0, len(plan.Changes))
	for _, change := range plan.Changes {
		decisions = append(decisions, decision{Change: change, Verdict: verdictStale})
	}
	return decisions
}

func allFailed(plan trackPlan) []decision {
	decisions := make([]decision, 0, len(plan.Changes))
	for _, change := range plan.Changes {
		decisions = append(decisions, decision{Change: change, Verdict: verdictFailed})
	}
	return decisions
}

func (s store) reconcileTrack(ctx context.Context, runID uuid.UUID, plan trackPlan, write bool) ([]decision, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return allFailed(plan), fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	decisions, err := decideInTx(ctx, tx, plan, write)
	if err != nil || !write {
		return decisions, err
	}
	if decisions, err = writeApplied(ctx, tx, runID, plan, decisions); err != nil {
		return allFailed(plan), err
	}
	if err := tx.Commit(ctx); err != nil {
		return allFailed(plan), fmt.Errorf("commit: %w", err)
	}
	return decisions, nil
}

func decideInTx(ctx context.Context, tx pgx.Tx, plan trackPlan, lock bool) ([]decision, error) {
	row, err := loadTrack(ctx, tx, plan, lock)
	if errors.Is(err, pgx.ErrNoRows) {
		return allStale(plan), nil
	}
	if err != nil {
		return allFailed(plan), fmt.Errorf("read track: %w", err)
	}
	if row.Version != plan.Version {
		return allStale(plan), nil
	}
	return decideTrack(plan, row), nil
}

func appliedOnly(decisions []decision) []decision {
	var applied []decision
	for _, d := range decisions {
		if d.Verdict == verdictApplied {
			applied = append(applied, d)
		}
	}
	return applied
}

func updateStatement(applied []decision) (string, []any) {
	args := []any{nil, nil, nil}
	assignments := make([]string, 0, len(applied))
	for _, d := range applied {
		args = append(args, d.Change.NewValue)
		placeholder := "$" + strconv.Itoa(len(args))
		if d.Change.Field == fieldYear {
			placeholder += "::text::int"
		}
		assignments = append(assignments, columnOf[d.Change.Field]+" = "+placeholder)
	}
	statement := "UPDATE tracks SET " + strings.Join(assignments, ", ") +
		", version = version + 1 WHERE id = $1 AND user_id = $2 AND version = $3"
	return statement, args
}

const insertChangeSQL = `INSERT INTO track_metadata_changes (run_id, track_id, user_id, field, old_value, new_value)
	VALUES ($1, $2, $3, $4, $5, $6)`

func writeApplied(ctx context.Context, tx pgx.Tx, runID uuid.UUID, plan trackPlan, decisions []decision) ([]decision, error) {
	applied := appliedOnly(decisions)
	if len(applied) == 0 {
		return decisions, nil
	}
	statement, args := updateStatement(applied)
	args[0], args[1], args[2] = plan.TrackID, plan.UserID, plan.Version
	tag, err := tx.Exec(ctx, statement, args...)
	if err != nil {
		return nil, fmt.Errorf("update track: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return allStale(plan), nil
	}
	for _, d := range applied {
		if _, err := tx.Exec(ctx, insertChangeSQL, runID, plan.TrackID, plan.UserID, string(d.Change.Field), d.Current, d.Change.NewValue); err != nil {
			return nil, fmt.Errorf("log change: %w", err)
		}
	}
	return decisions, nil
}

type loggedChange struct {
	TrackID  string
	UserID   string
	Field    field
	OldValue *string
	NewValue *string
}

type undoVerdict string

const (
	undoRestored       undoVerdict = "restored"
	undoSkippedChanged undoVerdict = "skipped-changed"
	undoFailed         undoVerdict = "failed"
)

type undoResult struct {
	Change  loggedChange
	Verdict undoVerdict
	Err     error
}

const loggedChangesSQL = `SELECT track_id::text, user_id::text, field, old_value, new_value
	FROM track_metadata_changes WHERE run_id = $1 AND undone_at IS NULL ORDER BY track_id, field`

func (s store) loggedChanges(ctx context.Context, runID uuid.UUID) ([]loggedChange, error) {
	rows, err := s.pool.Query(ctx, loggedChangesSQL, runID)
	if err != nil {
		return nil, fmt.Errorf("query change log: %w", err)
	}
	defer rows.Close()
	var changes []loggedChange
	for rows.Next() {
		var change loggedChange
		var rawField string
		if err := rows.Scan(&change.TrackID, &change.UserID, &rawField, &change.OldValue, &change.NewValue); err != nil {
			return nil, fmt.Errorf("scan change log: %w", err)
		}
		if change.Field, err = parseField(rawField); err != nil {
			return nil, err
		}
		changes = append(changes, change)
	}
	return changes, rows.Err()
}

func sameValue(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func (s store) undoChange(ctx context.Context, runID uuid.UUID, change loggedChange) (undoVerdict, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return undoFailed, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	verdict, err := checkUnchanged(ctx, tx, change)
	if verdict != undoRestored {
		return verdict, err
	}
	if err := restoreAndStamp(ctx, tx, runID, change); err != nil {
		return undoFailed, err
	}
	if err := tx.Commit(ctx); err != nil {
		return undoFailed, fmt.Errorf("commit: %w", err)
	}
	return undoRestored, nil
}

func (s store) undoRun(ctx context.Context, runID uuid.UUID) ([]undoResult, error) {
	changes, err := s.loggedChanges(ctx, runID)
	if err != nil {
		return nil, err
	}
	if len(changes) == 0 {
		return nil, fmt.Errorf("%w for run_id %s", errNothingToUndo, runID)
	}
	results := make([]undoResult, 0, len(changes))
	for _, change := range changes {
		verdict, err := s.undoChange(ctx, runID, change)
		results = append(results, undoResult{Change: change, Verdict: verdict, Err: err})
	}
	return results, nil
}

func restoreAndStamp(ctx context.Context, tx pgx.Tx, runID uuid.UUID, change loggedChange) error {
	placeholder := "$3"
	if change.Field == fieldYear {
		placeholder = "$3::text::int"
	}
	restore := "UPDATE tracks SET " + columnOf[change.Field] + " = " + placeholder + ", version = version + 1 WHERE id = $1 AND user_id = $2"
	if _, err := tx.Exec(ctx, restore, change.TrackID, change.UserID, change.OldValue); err != nil {
		return fmt.Errorf("restore track: %w", err)
	}
	if _, err := tx.Exec(ctx, "UPDATE track_metadata_changes SET undone_at = now() WHERE run_id = $1 AND track_id = $2 AND field = $3", runID, change.TrackID, string(change.Field)); err != nil {
		return fmt.Errorf("stamp undone_at: %w", err)
	}
	return nil
}

func checkUnchanged(ctx context.Context, tx pgx.Tx, change loggedChange) (undoVerdict, error) {
	var current *string
	err := tx.QueryRow(ctx, "SELECT "+columnOf[change.Field]+"::text FROM tracks WHERE id = $1 AND user_id = $2 FOR UPDATE", change.TrackID, change.UserID).Scan(&current)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return undoSkippedChanged, nil
	case err != nil:
		return undoFailed, fmt.Errorf("read track: %w", err)
	case !sameValue(current, change.NewValue):
		return undoSkippedChanged, nil
	}
	return undoRestored, nil
}
