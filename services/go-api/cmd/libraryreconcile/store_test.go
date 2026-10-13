package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testStore(t *testing.T) store {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return store{pool: pool}
}

type seeded struct {
	trackID string
	userID  string
}

func seedTrack(t *testing.T, s store, year *int, albumArtist, isrc *string) seeded {
	t.Helper()
	row := seeded{trackID: uuid.NewString(), userID: uuid.NewString()}
	_, err := s.pool.Exec(context.Background(),
		`INSERT INTO tracks (id, user_id, title, artist, dedup_key, year, album_artist, isrc, version)
		 VALUES ($1, $2, 't', 'a', $3, $4, $5, $6, 4)`,
		row.trackID, row.userID, row.trackID, year, albumArtist, isrc)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	t.Cleanup(func() {
		_, _ = s.pool.Exec(context.Background(), "DELETE FROM track_metadata_changes WHERE track_id = $1", row.trackID)
		_, _ = s.pool.Exec(context.Background(), "DELETE FROM tracks WHERE id = $1", row.trackID)
	})
	return row
}

func planFor(row seeded, version int, changes ...plannedChange) trackPlan {
	for i := range changes {
		changes[i].TrackID, changes[i].UserID, changes[i].SeenVersion = row.trackID, row.userID, version
	}
	return trackPlan{TrackID: row.trackID, UserID: row.userID, Version: version, Changes: changes}
}

type trackSnapshot struct {
	year        *int
	albumArtist *string
	isrc        *string
	version     int
}

func snapshot(t *testing.T, s store, row seeded) trackSnapshot {
	t.Helper()
	var got trackSnapshot
	err := s.pool.QueryRow(context.Background(),
		"SELECT year, album_artist, isrc, version FROM tracks WHERE id = $1", row.trackID).
		Scan(&got.year, &got.albumArtist, &got.isrc, &got.version)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func reconcile(t *testing.T, s store, write bool, plans ...trackPlan) (uuid.UUID, string, error) {
	t.Helper()
	runID := uuid.New()
	var out bytes.Buffer
	err := runReconcile(context.Background(), s, plans, write, runID, &out)
	return runID, out.String(), err
}

func logRows(t *testing.T, s store, runID uuid.UUID) int {
	t.Helper()
	var count int
	if err := s.pool.QueryRow(context.Background(), "SELECT count(*) FROM track_metadata_changes WHERE run_id = $1", runID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func fillChanges() []plannedChange {
	return []plannedChange{
		{Field: fieldYear, NewValue: text("2019")},
		{Field: fieldAlbumArtist, NewValue: text("Daniel Caesar")},
	}
}

func TestRunReconcile_ApplyFillsEmptyFieldsBumpsVersionAndLogsEachChange(t *testing.T) {
	s := testStore(t)
	row := seedTrack(t, s, nil, nil, nil)
	runID, out, err := reconcile(t, s, true, planFor(row, 4, fillChanges()...))
	got := snapshot(t, s, row)
	if err != nil || got.year == nil || *got.year != 2019 || *got.albumArtist != "Daniel Caesar" || got.version != 5 {
		t.Fatalf("err=%v got=%+v out=%s", err, got, out)
	}
	if logRows(t, s, runID) != 2 || !strings.Contains(out, "applied:           2") {
		t.Fatalf("log rows=%d out=%s", logRows(t, s, runID), out)
	}
}

func TestRunReconcile_DryRunWritesNothingAndReportsWhatWouldChange(t *testing.T) {
	s := testStore(t)
	row := seedTrack(t, s, nil, nil, nil)
	runID, out, err := reconcile(t, s, false, planFor(row, 4, fillChanges()...))
	got := snapshot(t, s, row)
	if err != nil || got.year != nil || got.albumArtist != nil || got.version != 4 || logRows(t, s, runID) != 0 {
		t.Fatalf("err=%v got=%+v", err, got)
	}
	if !strings.Contains(out, "would apply") || !strings.Contains(out, "DRY RUN") {
		t.Fatalf("out=%s", out)
	}
}

func TestRunReconcile_NeverOverwritesAFieldThatIsNoLongerEmpty(t *testing.T) {
	s := testStore(t)
	year := 1999
	row := seedTrack(t, s, &year, nil, nil)
	runID, out, err := reconcile(t, s, true, planFor(row, 4, fillChanges()...))
	got := snapshot(t, s, row)
	if err != nil || *got.year != 1999 || *got.albumArtist != "Daniel Caesar" || logRows(t, s, runID) != 1 {
		t.Fatalf("err=%v got=%+v out=%s", err, got, out)
	}
	if !strings.Contains(out, "skipped-not-empty: 1") {
		t.Fatalf("out=%s", out)
	}
}

func TestRunReconcile_SkipsATrackThatChangedSinceTheAudit(t *testing.T) {
	s := testStore(t)
	row := seedTrack(t, s, nil, nil, nil)
	runID, out, err := reconcile(t, s, true, planFor(row, 3, fillChanges()...))
	got := snapshot(t, s, row)
	if err != nil || got.year != nil || got.version != 4 || logRows(t, s, runID) != 0 || !strings.Contains(out, "stale:             2") {
		t.Fatalf("err=%v got=%+v out=%s", err, got, out)
	}
}

func TestRunReconcile_ClearsAWrongISRCOnlyWhileItStillMatchesTheAudit(t *testing.T) {
	s := testStore(t)
	wrong := text("USWRONG00001")
	matching := seedTrack(t, s, nil, nil, wrong)
	changed := seedTrack(t, s, nil, nil, text("USOTHER00001"))
	clear := plannedChange{Field: fieldISRC, AuditedOld: wrong}
	runID, out, err := reconcile(t, s, true, planFor(matching, 4, clear), planFor(changed, 4, clear))
	if err != nil || snapshot(t, s, matching).isrc != nil || *snapshot(t, s, changed).isrc != "USOTHER00001" || logRows(t, s, runID) != 1 {
		t.Fatalf("err=%v out=%s", err, out)
	}
}

func TestRunReconcile_ApplyingTheSameReportTwiceChangesNothingTheSecondTime(t *testing.T) {
	s := testStore(t)
	row := seedTrack(t, s, nil, nil, nil)
	plan := planFor(row, 4, fillChanges()...)
	reconcile(t, s, true, plan)
	runID, out, err := reconcile(t, s, true, plan)
	if err != nil || logRows(t, s, runID) != 0 || snapshot(t, s, row).version != 5 || !strings.Contains(out, "stale:             2") {
		t.Fatalf("err=%v out=%s", err, out)
	}
}

func TestRunReconcile_AMissingTrackIsStaleAndTheRunContinues(t *testing.T) {
	s := testStore(t)
	row := seedTrack(t, s, nil, nil, nil)
	gone := seeded{trackID: uuid.NewString(), userID: uuid.NewString()}
	runID, out, err := reconcile(t, s, true, planFor(gone, 1, fillChanges()...), planFor(row, 4, fillChanges()...))
	if err != nil || logRows(t, s, runID) != 2 || !strings.Contains(out, "stale:             2") {
		t.Fatalf("err=%v out=%s", err, out)
	}
}

func TestRunUndo_RestoresOldValuesAndStampsUndoneAt(t *testing.T) {
	s := testStore(t)
	wrong := text("USWRONG00001")
	row := seedTrack(t, s, nil, nil, wrong)
	runID, _, _ := reconcile(t, s, true, planFor(row, 4, plannedChange{Field: fieldISRC, AuditedOld: wrong}))
	var out bytes.Buffer
	err := runUndo(context.Background(), s, runID, &out)
	got := snapshot(t, s, row)
	var stamped int
	_ = s.pool.QueryRow(context.Background(), "SELECT count(*) FROM track_metadata_changes WHERE run_id = $1 AND undone_at IS NOT NULL", runID).Scan(&stamped)
	if err != nil || got.isrc == nil || *got.isrc != "USWRONG00001" || got.version != 6 || stamped != 1 || !strings.Contains(out.String(), "restored:        1") {
		t.Fatalf("err=%v got=%+v stamped=%d out=%s", err, got, stamped, out.String())
	}
}

func TestRunUndo_RestoresAFilledFieldToEmpty(t *testing.T) {
	s := testStore(t)
	row := seedTrack(t, s, nil, nil, nil)
	runID, _, _ := reconcile(t, s, true, planFor(row, 4, fillChanges()...))
	var out bytes.Buffer
	err := runUndo(context.Background(), s, runID, &out)
	got := snapshot(t, s, row)
	if err != nil || got.year != nil || got.albumArtist != nil {
		t.Fatalf("err=%v got=%+v out=%s", err, got, out.String())
	}
}

func TestRunUndo_SkipsAFieldEditedAfterTheRunAndOnlyOnce(t *testing.T) {
	s := testStore(t)
	row := seedTrack(t, s, nil, nil, nil)
	runID, _, _ := reconcile(t, s, true, planFor(row, 4, fillChanges()...))
	if _, err := s.pool.Exec(context.Background(), "UPDATE tracks SET album_artist = 'Edited by user' WHERE id = $1", row.trackID); err != nil {
		t.Fatal(err)
	}
	var first, second bytes.Buffer
	_ = runUndo(context.Background(), s, runID, &first)
	_ = runUndo(context.Background(), s, runID, &second)
	got := snapshot(t, s, row)
	if *got.albumArtist != "Edited by user" || got.year != nil {
		t.Fatalf("got=%+v", got)
	}
	if !strings.Contains(first.String(), "skipped-changed: 1") || !strings.Contains(second.String(), "skipped-changed: 1") || !strings.Contains(second.String(), "restored:        0") {
		t.Fatalf("first=%s second=%s", first.String(), second.String())
	}
}

func TestRunUndo_AnUnknownOrAlreadyUndoneRunIsAnError(t *testing.T) {
	s := testStore(t)
	row := seedTrack(t, s, nil, nil, nil)
	runID, _, _ := reconcile(t, s, true, planFor(row, 4, fillChanges()...))
	var out bytes.Buffer
	if err := runUndo(context.Background(), s, runID, &out); err != nil {
		t.Fatal(err)
	}
	for name, id := range map[string]uuid.UUID{"already undone": runID, "unknown": uuid.New()} {
		if err := runUndo(context.Background(), s, id, &out); !errors.Is(err, errNothingToUndo) {
			t.Fatalf("%s: err=%v", name, err)
		}
	}
}
