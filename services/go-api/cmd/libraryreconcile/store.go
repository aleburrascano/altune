package main

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared/songfacts"
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
	Other   string
}

type store struct {
	pool   *pgxpool.Pool
	access pgx.TxAccessMode
}

func (s store) begin(ctx context.Context) (pgx.Tx, error) {
	return s.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: s.access})
}

type trackRow struct {
	Values   map[field]*string
	Version  int
	DedupKey string
}

type keyChange struct {
	Old string
	New string
}

const selectTrackSQL = `SELECT year::text, album_artist, isrc, track_number::text, genre, catalog_duration_seconds::text, artwork_url, title, artist, album, dedup_key, version FROM tracks WHERE id = $1 AND user_id = $2`

func loadTrack(ctx context.Context, tx pgx.Tx, plan trackPlan, lock bool) (trackRow, error) {
	query := selectTrackSQL
	if lock {
		query += " FOR UPDATE"
	}
	var year, albumArtist, isrc, trackNumber, genre, catalogLength, artworkURL, title, artist, album *string
	var row trackRow
	if err := tx.QueryRow(ctx, query, plan.TrackID, plan.UserID).Scan(&year, &albumArtist, &isrc, &trackNumber, &genre, &catalogLength, &artworkURL, &title, &artist, &album, &row.DedupKey, &row.Version); err != nil {
		return trackRow{}, err
	}
	row.Values = map[field]*string{
		fieldYear: year, fieldAlbumArtist: albumArtist, fieldISRC: isrc,
		fieldTrackNumber: trackNumber, fieldGenre: genre, fieldCatalogLen: catalogLength, fieldArtworkURL: artworkURL,
		fieldTitle: title, fieldArtist: artist, fieldAlbum: album,
	}
	return row, nil
}

func decideTrack(plan trackPlan, row trackRow) []decision {
	decisions := make([]decision, 0, len(plan.Changes))
	for _, change := range plan.Changes {
		current := row.Values[change.Field]
		decisions = append(decisions, decision{Change: change, Verdict: decide(change, current), Current: current})
	}
	return holdGroupsTogether(decisions)
}

func holdGroupsTogether(decisions []decision) []decision {
	blocked := map[string]bool{}
	for _, d := range decisions {
		if group := d.Change.effectiveGroup(); group != "" && d.Verdict != verdictApplied {
			blocked[group] = true
		}
	}
	for i, d := range decisions {
		if d.Verdict == verdictApplied && blocked[d.Change.effectiveGroup()] {
			decisions[i].Verdict = verdictSkippedGroup
		}
	}
	return decisions
}

func allWith(plan trackPlan, v verdict) []decision {
	decisions := make([]decision, 0, len(plan.Changes))
	for _, change := range plan.Changes {
		decisions = append(decisions, decision{Change: change, Verdict: v})
	}
	return decisions
}

func allStale(plan trackPlan) []decision {
	return allWith(plan, verdictStale)
}

func allFailed(plan trackPlan) []decision {
	return allWith(plan, verdictFailed)
}

func (s store) reconcileTrack(ctx context.Context, runID uuid.UUID, plan trackPlan, write bool) ([]decision, error) {
	tx, err := s.begin(ctx)
	if err != nil {
		return allFailed(plan), fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	decisions, key, err := decideInTx(ctx, tx, plan, write)
	if err != nil || !write {
		return decisions, err
	}
	if decisions, err = writeApplied(ctx, tx, runID, plan, decisions, key); err != nil {
		return allFailed(plan), err
	}
	if err := tx.Commit(ctx); err != nil {
		return allFailed(plan), fmt.Errorf("commit: %w", err)
	}
	return decisions, nil
}

func decideInTx(ctx context.Context, tx pgx.Tx, plan trackPlan, lock bool) ([]decision, *keyChange, error) {
	row, err := loadTrack(ctx, tx, plan, lock)
	if errors.Is(err, pgx.ErrNoRows) {
		return allStale(plan), nil, nil
	}
	if err != nil {
		return allFailed(plan), nil, fmt.Errorf("read track: %w", err)
	}
	if row.Version != plan.Version {
		return allStale(plan), nil, nil
	}
	decisions, key, err := settleDedupKey(ctx, tx, plan, row, decideTrack(plan, row))
	if err != nil {
		return decisions, key, err
	}
	decisions, err = refuseHeldISRC(ctx, tx, plan, decisions)
	return decisions, key, err
}

func refuseHeldISRC(ctx context.Context, tx pgx.Tx, plan trackPlan, decisions []decision) ([]decision, error) {
	for i, d := range decisions {
		if d.Verdict != verdictApplied || d.Change.Field != fieldISRC || d.Change.NewValue == nil {
			continue
		}
		var other string
		err := tx.QueryRow(ctx, "SELECT id::text FROM tracks WHERE user_id = $1 AND upper(replace(replace(isrc, '-', ''), ' ', '')) = $2 AND id <> $3 LIMIT 1", plan.UserID, songfacts.CanonicalISRC(*d.Change.NewValue), plan.TrackID).Scan(&other)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
		case err != nil:
			return allFailed(plan), fmt.Errorf("check isrc holder: %w", err)
		default:
			decisions[i].Verdict, decisions[i].Other = verdictSkippedDuplicate, other
		}
	}
	return decisions, nil
}

func settleDedupKey(ctx context.Context, tx pgx.Tx, plan trackPlan, row trackRow, decisions []decision) ([]decision, *keyChange, error) {
	if !appliesIdentity(decisions) {
		return decisions, nil, nil
	}
	newKey := recomputedKey(row, decisions)
	if newKey == row.DedupKey {
		return decisions, nil, nil
	}
	var other string
	err := tx.QueryRow(ctx, "SELECT id::text FROM tracks WHERE user_id = $1 AND dedup_key = $2 AND id <> $3 LIMIT 1", plan.UserID, newKey, plan.TrackID).Scan(&other)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return decisions, &keyChange{Old: row.DedupKey, New: newKey}, nil
	case err != nil:
		return allFailed(plan), nil, fmt.Errorf("check dedup clash: %w", err)
	}
	return skipIdentityGroup(decisions, other), nil, nil
}

func appliesIdentity(decisions []decision) bool {
	for _, d := range decisions {
		if d.Verdict == verdictApplied && isIdentity(d.Change.Field) {
			return true
		}
	}
	return false
}

func recomputedKey(row trackRow, decisions []decision) string {
	values := map[field]string{}
	for _, f := range []field{fieldTitle, fieldArtist, fieldAlbum} {
		if current := row.Values[f]; current != nil {
			values[f] = *current
		}
	}
	for _, d := range decisions {
		if d.Verdict == verdictApplied && isIdentity(d.Change.Field) {
			values[d.Change.Field] = *d.Change.NewValue
		}
	}
	return domain.TrackDedupKey(values[fieldTitle], values[fieldArtist], values[fieldAlbum])
}

func skipIdentityGroup(decisions []decision, other string) []decision {
	group := ""
	for _, d := range decisions {
		if isIdentity(d.Change.Field) {
			group = d.Change.effectiveGroup()
		}
	}
	for i, d := range decisions {
		if d.Verdict == verdictApplied && d.Change.effectiveGroup() == group {
			decisions[i].Verdict, decisions[i].Other = verdictSkippedDuplicate, other
		}
	}
	return decisions
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

func updateStatement(applied []decision, key *keyChange) (string, []any) {
	args := []any{nil, nil, nil}
	assignments := make([]string, 0, len(applied)+1)
	for _, d := range applied {
		args = append(args, d.Change.NewValue)
		placeholder := "$" + strconv.Itoa(len(args)) + castOf[d.Change.Field]
		assignments = append(assignments, columnOf[d.Change.Field]+" = "+placeholder)
	}
	if key != nil {
		args = append(args, key.New)
		assignments = append(assignments, "dedup_key = $"+strconv.Itoa(len(args)))
	}
	statement := "UPDATE tracks SET " + strings.Join(assignments, ", ") +
		", version = version + 1 WHERE id = $1 AND user_id = $2 AND version = $3"
	return statement, args
}

const insertChangeSQL = `INSERT INTO track_metadata_changes (run_id, track_id, user_id, field, old_value, new_value, change_group)
	VALUES ($1, $2, $3, $4, $5, $6, $7)`

func groupArg(group string) *string {
	if group == "" {
		return nil
	}
	return &group
}

func writeApplied(ctx context.Context, tx pgx.Tx, runID uuid.UUID, plan trackPlan, decisions []decision, key *keyChange) ([]decision, error) {
	applied := appliedOnly(decisions)
	if len(applied) == 0 {
		return decisions, nil
	}
	statement, args := updateStatement(applied, key)
	args[0], args[1], args[2] = plan.TrackID, plan.UserID, plan.Version
	tag, err := tx.Exec(ctx, statement, args...)
	if err != nil {
		return nil, fmt.Errorf("update track: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return allStale(plan), nil
	}
	if err := logChanges(ctx, tx, runID, plan, applied, key); err != nil {
		return nil, err
	}
	return decisions, nil
}

func logChanges(ctx context.Context, tx pgx.Tx, runID uuid.UUID, plan trackPlan, applied []decision, key *keyChange) error {
	for _, d := range applied {
		if _, err := tx.Exec(ctx, insertChangeSQL, runID, plan.TrackID, plan.UserID, string(d.Change.Field), d.Current, d.Change.NewValue, groupArg(d.Change.effectiveGroup())); err != nil {
			return fmt.Errorf("log change: %w", err)
		}
	}
	if key == nil {
		return nil
	}
	if _, err := tx.Exec(ctx, insertChangeSQL, runID, plan.TrackID, plan.UserID, string(fieldDedupKey), key.Old, key.New, identityGroupOf(applied)); err != nil {
		return fmt.Errorf("log dedup key: %w", err)
	}
	return nil
}

func identityGroupOf(applied []decision) *string {
	for _, d := range applied {
		if isIdentity(d.Change.Field) {
			return groupArg(d.Change.effectiveGroup())
		}
	}
	return nil
}

type loggedChange struct {
	TrackID  string
	UserID   string
	Field    field
	OldValue *string
	NewValue *string
	Group    string
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

const loggedChangesSQL = `SELECT track_id::text, user_id::text, field, old_value, new_value, COALESCE(change_group, '')
	FROM track_metadata_changes WHERE run_id = $1 AND undone_at IS NULL ORDER BY track_id, field`

func (s store) loggedChanges(ctx context.Context, runID uuid.UUID) ([]loggedChange, error) {
	tx, err := s.begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, loggedChangesSQL, runID)
	if err != nil {
		return nil, fmt.Errorf("query change log: %w", err)
	}
	defer rows.Close()
	var changes []loggedChange
	for rows.Next() {
		var change loggedChange
		var rawField string
		if err := rows.Scan(&change.TrackID, &change.UserID, &rawField, &change.OldValue, &change.NewValue, &change.Group); err != nil {
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

func (s store) undoUnit(ctx context.Context, runID uuid.UUID, unit []loggedChange) (undoVerdict, error) {
	tx, err := s.begin(ctx)
	if err != nil {
		return undoFailed, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, change := range unit {
		verdict, err := checkUnchanged(ctx, tx, change)
		if verdict != undoRestored {
			return verdict, err
		}
	}
	for _, change := range unit {
		if err := restoreAndStamp(ctx, tx, runID, change); err != nil {
			return undoFailed, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return undoFailed, fmt.Errorf("commit: %w", err)
	}
	return undoRestored, nil
}

func undoUnits(changes []loggedChange) [][]loggedChange {
	var units [][]loggedChange
	position := map[string]int{}
	for _, change := range changes {
		if change.Group == "" {
			units = append(units, []loggedChange{change})
			continue
		}
		key := change.TrackID + "/" + change.Group
		index, seen := position[key]
		if !seen {
			index = len(units)
			position[key] = index
			units = append(units, nil)
		}
		units[index] = append(units[index], change)
	}
	return units
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
	for _, unit := range undoUnits(changes) {
		verdict, err := s.undoUnit(ctx, runID, unit)
		for _, change := range unit {
			results = append(results, undoResult{Change: change, Verdict: verdict, Err: err})
		}
	}
	return results, nil
}

func restoreAndStamp(ctx context.Context, tx pgx.Tx, runID uuid.UUID, change loggedChange) error {
	placeholder := "$3" + castOf[change.Field]
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
