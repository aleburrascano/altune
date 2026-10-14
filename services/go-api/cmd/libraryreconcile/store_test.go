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

func TestRunReconcile_FillsAnISRCAndUndoRestoresItToEmpty(t *testing.T) {
	s := testStore(t)
	empty := seedTrack(t, s, nil, nil, nil)
	taken := seedTrack(t, s, nil, nil, text("USOTHER00001"))
	fill := plannedChange{Field: fieldISRC, NewValue: text("USFOUND00001")}
	runID, out, err := reconcile(t, s, true, planFor(empty, 4, fill), planFor(taken, 4, fill))
	if err != nil || *snapshot(t, s, empty).isrc != "USFOUND00001" || *snapshot(t, s, taken).isrc != "USOTHER00001" || logRows(t, s, runID) != 1 {
		t.Fatalf("err=%v out=%s", err, out)
	}
	var undone bytes.Buffer
	if err := runUndo(context.Background(), s, runID, &undone); err != nil || snapshot(t, s, empty).isrc != nil {
		t.Fatalf("err=%v out=%s", err, undone.String())
	}
}

func TestRunReconcile_FillsTrackNumberGenreAndCatalogLengthThenUndoRestoresThem(t *testing.T) {
	s := testStore(t)
	empty := seedTrack(t, s, nil, nil, nil)
	filled := seedTrack(t, s, nil, nil, nil)
	_, err := s.pool.Exec(context.Background(), "UPDATE tracks SET track_number = 2, genre = 'Pop', catalog_duration_seconds = 100 WHERE id = $1", filled.trackID)
	if err != nil {
		t.Fatal(err)
	}
	changes := func() []plannedChange {
		return []plannedChange{
			{Field: fieldTrackNumber, NewValue: text("4")},
			{Field: fieldGenre, NewValue: text("R&B")},
			{Field: fieldCatalogLen, NewValue: text("236.5")},
		}
	}
	runID, out, err := reconcile(t, s, true, planFor(empty, 4, changes()...), planFor(filled, 4, changes()...))
	if err != nil || logRows(t, s, runID) != 3 {
		t.Fatalf("err=%v rows=%d out=%s", err, logRows(t, s, runID), out)
	}
	stored := func(row seeded) (number *int, genre *string, length *float64) {
		_ = s.pool.QueryRow(context.Background(), "SELECT track_number, genre, catalog_duration_seconds FROM tracks WHERE id = $1", row.trackID).Scan(&number, &genre, &length)
		return number, genre, length
	}
	number, genre, length := stored(empty)
	keptNumber, keptGenre, keptLength := stored(filled)
	if *number != 4 || *genre != "R&B" || *length != 236.5 || *keptNumber != 2 || *keptGenre != "Pop" || *keptLength != 100 {
		t.Fatalf("filled=%v %v %v kept=%v %v %v", *number, *genre, *length, *keptNumber, *keptGenre, *keptLength)
	}
	var undone bytes.Buffer
	if err := runUndo(context.Background(), s, runID, &undone); err != nil {
		t.Fatalf("err=%v out=%s", err, undone.String())
	}
	if number, genre, length = stored(empty); number != nil || genre != nil || length != nil {
		t.Fatalf("undo left %v %v %v", number, genre, length)
	}
}

func TestRunReconcile_DryRunOfCatalogFieldsWritesNothing(t *testing.T) {
	s := testStore(t)
	row := seedTrack(t, s, nil, nil, nil)
	runID, out, err := reconcile(t, s, false, planFor(row, 4, plannedChange{Field: fieldGenre, NewValue: text("R&B")}))
	if err != nil || logRows(t, s, runID) != 0 || snapshot(t, s, row).version != 4 || !strings.Contains(out, "would apply:") {
		t.Fatalf("err=%v out=%s", err, out)
	}
}

func TestRunReconcile_ReplacesAnArtworkUrlOnlyWhileItMatchesTheAuditAndUndoRestoresIt(t *testing.T) {
	s := testStore(t)
	matching := seedTrack(t, s, nil, nil, nil)
	changed := seedTrack(t, s, nil, nil, nil)
	for row, url := range map[seeded]string{matching: "https://lib/a.jpg", changed: "https://lib/edited.jpg"} {
		if _, err := s.pool.Exec(context.Background(), "UPDATE tracks SET artwork_url = $2 WHERE id = $1", row.trackID, url); err != nil {
			t.Fatal(err)
		}
	}
	replace := plannedChange{Field: fieldArtworkURL, AuditedOld: text("https://lib/a.jpg"), NewValue: text("https://cdn/b.jpg")}
	artwork := func(row seeded) string {
		var url string
		_ = s.pool.QueryRow(context.Background(), "SELECT artwork_url FROM tracks WHERE id = $1", row.trackID).Scan(&url)
		return url
	}
	runID, out, err := reconcile(t, s, true, planFor(matching, 4, replace), planFor(changed, 4, replace))
	if err != nil || artwork(matching) != "https://cdn/b.jpg" || artwork(changed) != "https://lib/edited.jpg" || logRows(t, s, runID) != 1 {
		t.Fatalf("err=%v out=%s", err, out)
	}
	var undone bytes.Buffer
	if err := runUndo(context.Background(), s, runID, &undone); err != nil || artwork(matching) != "https://lib/a.jpg" {
		t.Fatalf("err=%v out=%s", err, undone.String())
	}
}

func TestRunReconcile_CorrectModeOverwritesOnlyTheDerivedValueAndUndoRestoresIt(t *testing.T) {
	s := testStore(t)
	remaster, edited := 2024, 2024
	matching := seedTrack(t, s, &remaster, nil, nil)
	changed := seedTrack(t, s, &edited, nil, nil)
	if _, err := s.pool.Exec(context.Background(), "UPDATE tracks SET year = 2010 WHERE id = $1", changed.trackID); err != nil {
		t.Fatal(err)
	}
	correct := plannedChange{Mode: modeCorrect, Field: fieldYear, AuditedOld: text("2024"), NewValue: text("1984")}
	runID, out, err := reconcile(t, s, true, planFor(matching, 4, correct), planFor(changed, 4, correct))
	if err != nil || *snapshot(t, s, matching).year != 1984 || *snapshot(t, s, changed).year != 2010 || logRows(t, s, runID) != 1 {
		t.Fatalf("err=%v out=%s", err, out)
	}
	if got := snapshot(t, s, matching); got.version != 5 {
		t.Fatalf("version %d", got.version)
	}
	var undone bytes.Buffer
	if err := runUndo(context.Background(), s, runID, &undone); err != nil || *snapshot(t, s, matching).year != 2024 {
		t.Fatalf("err=%v out=%s", err, undone.String())
	}
}

func TestRunReconcile_CorrectModeDryRunWritesNothing(t *testing.T) {
	s := testStore(t)
	year := 2024
	row := seedTrack(t, s, &year, nil, nil)
	correct := plannedChange{Mode: modeCorrect, Field: fieldYear, AuditedOld: text("2024"), NewValue: text("1984")}
	runID, out, err := reconcile(t, s, false, planFor(row, 4, correct))
	if err != nil || *snapshot(t, s, row).year != 2024 || logRows(t, s, runID) != 0 || !strings.Contains(out, "would apply:") {
		t.Fatalf("err=%v out=%s", err, out)
	}
}

type songRow struct {
	Title, Artist, Album, Genre, TrackNumber, ArtworkURL, AlbumArtist, Year, DedupKey string
	Version                                                                           int
}

func seedSong(t *testing.T, s store, userID, title, artist, album, key string) seeded {
	t.Helper()
	row := seeded{trackID: uuid.NewString(), userID: userID}
	_, err := s.pool.Exec(context.Background(),
		`INSERT INTO tracks (id, user_id, title, artist, album, dedup_key, genre, track_number, artwork_url, album_artist, year, version)
		 VALUES ($1, $2, $3, $4, $5, $6, 'Pop', 12, 'https://cdn/now.jpg', 'Various Artists', 1999, 4)`,
		row.trackID, row.userID, title, artist, album, key)
	if err != nil {
		t.Fatalf("seed song: %v", err)
	}
	t.Cleanup(func() {
		_, _ = s.pool.Exec(context.Background(), "DELETE FROM track_metadata_changes WHERE track_id = $1", row.trackID)
		_, _ = s.pool.Exec(context.Background(), "DELETE FROM tracks WHERE id = $1", row.trackID)
	})
	return row
}

func readSong(t *testing.T, s store, row seeded) songRow {
	t.Helper()
	var got songRow
	err := s.pool.QueryRow(context.Background(),
		`SELECT title, artist, album, genre, track_number::text, artwork_url, album_artist, year::text, dedup_key, version FROM tracks WHERE id = $1`, row.trackID).
		Scan(&got.Title, &got.Artist, &got.Album, &got.Genre, &got.TrackNumber, &got.ArtworkURL, &got.AlbumArtist, &got.Year, &got.DedupKey, &got.Version)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func albumMove(group string) []plannedChange {
	correct := func(f field, old, value string) plannedChange {
		return plannedChange{Mode: modeCorrect, Field: f, AuditedOld: text(old), NewValue: text(value), Group: group}
	}
	return []plannedChange{
		correct(fieldAlbum, "Now 14", "Simple Pleasures"),
		correct(fieldTrackNumber, "12", "2"),
		correct(fieldGenre, "Pop", "Jazz"),
		correct(fieldArtworkURL, "https://cdn/now.jpg", "https://cdn/sp.jpg"),
		correct(fieldAlbumArtist, "Various Artists", "Bobby McFerrin"),
		correct(fieldYear, "1999", "1988"),
	}
}

const (
	nowKey = "dont worry be happy|bobby mcferrin|now 14"
	spKey  = "dont worry be happy|bobby mcferrin|simple pleasures"
)

func movedSong() songRow {
	return songRow{
		Title: "Don't Worry, Be Happy", Artist: "Bobby McFerrin", Album: "Simple Pleasures", Genre: "Jazz", TrackNumber: "2",
		ArtworkURL: "https://cdn/sp.jpg", AlbumArtist: "Bobby McFerrin", Year: "1988", DedupKey: spKey, Version: 5,
	}
}

func originalSong() songRow {
	return songRow{
		Title: "Don't Worry, Be Happy", Artist: "Bobby McFerrin", Album: "Now 14", Genre: "Pop", TrackNumber: "12",
		ArtworkURL: "https://cdn/now.jpg", AlbumArtist: "Various Artists", Year: "1999", DedupKey: nowKey, Version: 4,
	}
}

func TestRunReconcile_AnAlbumMoveAppliesItsDependentsAndRecomputesTheDedupKeyInOneStep(t *testing.T) {
	s := testStore(t)
	row := seedSong(t, s, uuid.NewString(), "Don't Worry, Be Happy", "Bobby McFerrin", "Now 14", nowKey)
	runID, out, err := reconcile(t, s, true, planFor(row, 4, albumMove("album-move:1")...))
	if got := readSong(t, s, row); err != nil || got != movedSong() || logRows(t, s, runID) != 7 {
		t.Fatalf("err=%v got=%+v out=%s", err, readSong(t, s, row), out)
	}
}

func TestRunReconcile_ADedupClashSkipsTheWholeGroupAndNamesTheOtherTrack(t *testing.T) {
	s := testStore(t)
	user := uuid.NewString()
	row := seedSong(t, s, user, "Don't Worry, Be Happy", "Bobby McFerrin", "Now 14", nowKey)
	other := seedSong(t, s, user, "Don't Worry, Be Happy", "Bobby McFerrin", "Simple Pleasures", spKey)
	for _, write := range []bool{false, true} {
		runID, out, err := reconcile(t, s, write, planFor(row, 4, albumMove("album-move:1")...))
		if err != nil || readSong(t, s, row) != originalSong() || logRows(t, s, runID) != 0 {
			t.Fatalf("write=%v err=%v got=%+v out=%s", write, err, readSong(t, s, row), out)
		}
		if !strings.Contains(out, "skipped-duplicate: 6") || !strings.Contains(out, "same song as track "+other.trackID) {
			t.Fatalf("write=%v out=%s", write, out)
		}
	}
	if readSong(t, s, other).DedupKey != spKey {
		t.Fatal("the other track must stay untouched")
	}
}

func TestRunReconcile_ASameKeyInAnotherLibraryIsNotAClash(t *testing.T) {
	s := testStore(t)
	row := seedSong(t, s, uuid.NewString(), "Don't Worry, Be Happy", "Bobby McFerrin", "Now 14", nowKey)
	seedSong(t, s, uuid.NewString(), "Don't Worry, Be Happy", "Bobby McFerrin", "Simple Pleasures", spKey)
	_, out, err := reconcile(t, s, true, planFor(row, 4, albumMove("album-move:1")...))
	if err != nil || readSong(t, s, row) != movedSong() {
		t.Fatalf("err=%v out=%s", err, out)
	}
}

func TestRunReconcile_AStaleOldValueInTheGroupSkipsEveryMember(t *testing.T) {
	s := testStore(t)
	row := seedSong(t, s, uuid.NewString(), "Don't Worry, Be Happy", "Bobby McFerrin", "Now 14", nowKey)
	if _, err := s.pool.Exec(context.Background(), "UPDATE tracks SET genre = 'Rock' WHERE id = $1", row.trackID); err != nil {
		t.Fatal(err)
	}
	runID, out, err := reconcile(t, s, true, planFor(row, 4, albumMove("album-move:1")...))
	want := originalSong()
	want.Genre = "Rock"
	if err != nil || readSong(t, s, row) != want || logRows(t, s, runID) != 0 {
		t.Fatalf("err=%v got=%+v out=%s", err, readSong(t, s, row), out)
	}
	if !strings.Contains(out, "skipped-group:     5") || !strings.Contains(out, "skipped-not-empty: 1") {
		t.Fatalf("out=%s", out)
	}
}

func TestRunUndo_AGroupRestoresEveryFieldAndTheDedupKey(t *testing.T) {
	s := testStore(t)
	row := seedSong(t, s, uuid.NewString(), "Don't Worry, Be Happy", "Bobby McFerrin", "Now 14", nowKey)
	runID, _, _ := reconcile(t, s, true, planFor(row, 4, albumMove("album-move:1")...))
	var out bytes.Buffer
	err := runUndo(context.Background(), s, runID, &out)
	want := originalSong()
	want.Version = 5 + 7
	if got := readSong(t, s, row); err != nil || got != want || !strings.Contains(out.String(), "restored:        7") {
		t.Fatalf("err=%v got=%+v out=%s", err, got, out.String())
	}
}

func TestRunUndo_AGroupWithOneMemberEditedSinceRestoresNothing(t *testing.T) {
	s := testStore(t)
	row := seedSong(t, s, uuid.NewString(), "Don't Worry, Be Happy", "Bobby McFerrin", "Now 14", nowKey)
	runID, _, _ := reconcile(t, s, true, planFor(row, 4, albumMove("album-move:1")...))
	if _, err := s.pool.Exec(context.Background(), "UPDATE tracks SET genre = 'Soul' WHERE id = $1", row.trackID); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err := runUndo(context.Background(), s, runID, &out)
	want := movedSong()
	want.Genre = "Soul"
	if got := readSong(t, s, row); err != nil || got != want || !strings.Contains(out.String(), "skipped-changed: 7") {
		t.Fatalf("err=%v got=%+v out=%s", err, got, out.String())
	}
}

func TestRunReconcile_TitleAndArtistCorrectionsRecomputeTheKeyAndUndoRestoresIt(t *testing.T) {
	s := testStore(t)
	row := seedSong(t, s, uuid.NewString(), "Wham! - Careless Whisper (Official Video)", "WhamVEVO", "Make It Big", "wham careless whisper official video|whamvevo|make it big")
	changes := []plannedChange{
		{Mode: modeCorrect, Field: fieldTitle, AuditedOld: text("Wham! - Careless Whisper (Official Video)"), NewValue: text("Careless Whisper")},
		{Mode: modeCorrect, Field: fieldArtist, AuditedOld: text("WhamVEVO"), NewValue: text("Wham!")},
	}
	runID, out, err := reconcile(t, s, true, planFor(row, 4, changes...))
	got := readSong(t, s, row)
	if err != nil || got.Title != "Careless Whisper" || got.Artist != "Wham!" || got.DedupKey != "careless whisper|wham|make it big" || logRows(t, s, runID) != 3 {
		t.Fatalf("err=%v got=%+v out=%s", err, got, out)
	}
	var undone bytes.Buffer
	got = readSong(t, s, row)
	if got.Version != 5 {
		t.Fatalf("version %d, want 5", got.Version)
	}
	if err := runUndo(context.Background(), s, runID, &undone); err != nil {
		t.Fatalf("err=%v out=%s", err, undone.String())
	}
	got = readSong(t, s, row)
	if got.Title != "Wham! - Careless Whisper (Official Video)" || got.Artist != "WhamVEVO" || got.DedupKey != "wham careless whisper official video|whamvevo|make it big" {
		t.Fatalf("got=%+v", got)
	}
}

func TestRunReconcile_ATitleCorrectionThatClashesWithAnotherTrackIsSkipped(t *testing.T) {
	s := testStore(t)
	user := uuid.NewString()
	row := seedSong(t, s, user, "Careless Whisper (Official Video)", "Wham!", "Make It Big", "careless whisper official video|wham|make it big")
	other := seedSong(t, s, user, "Careless Whisper", "Wham!", "Make It Big", "careless whisper|wham|make it big")
	change := plannedChange{Mode: modeCorrect, Field: fieldTitle, AuditedOld: text("Careless Whisper (Official Video)"), NewValue: text("Careless Whisper")}
	runID, out, err := reconcile(t, s, true, planFor(row, 4, change))
	if got := readSong(t, s, row); err != nil || got.Title != "Careless Whisper (Official Video)" || logRows(t, s, runID) != 0 ||
		!strings.Contains(out, "skipped-duplicate: 1") || !strings.Contains(out, other.trackID) {
		t.Fatalf("err=%v got=%+v out=%s", err, got, out)
	}
}
