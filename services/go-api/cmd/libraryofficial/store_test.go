package main

import (
	"bytes"
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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

func seedBound(t *testing.T, s store, userID, sources string) target {
	t.Helper()
	track := theTrack()
	track.TrackID, track.UserID = uuid.NewString(), userID
	ctx := context.Background()
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO tracks (id, user_id, title, artist, album, dedup_key, isrc, duration_seconds, catalog_duration_seconds, acquisition_status, audio_ref, audio_version, version, catalog_sources)
		 VALUES ($1, $2, $3, $4, $5, $10, $6, 281, $7, 'ready', 'audio/a.mp3', 'v1', $8, $9::jsonb)`,
		track.TrackID, track.UserID, track.Title, track.Artist, track.Album, track.ISRC, int(track.CatalogSeconds), track.Version, sources, track.TrackID); err != nil {
		t.Fatalf("seed track: %v", err)
	}
	t.Cleanup(func() { _, _ = s.pool.Exec(ctx, "DELETE FROM tracks WHERE id = $1", track.TrackID) })
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO track_identity (track_id, user_id, isrc, status, reason, evidence, run_id, track_version_at_bind)
		 VALUES ($1, $2, $3, 'bound', 'seeded', '{}', $4, $5)`,
		track.TrackID, track.UserID, track.ISRC, uuid.New(), track.Version); err != nil {
		t.Fatalf("seed identity: %v", err)
	}
	return track
}

func sourcesOf(t *testing.T, s store, trackID string) string {
	t.Helper()
	var sources string
	if err := s.pool.QueryRow(context.Background(), "SELECT catalog_sources::text FROM tracks WHERE id = $1", trackID).Scan(&sources); err != nil {
		t.Fatal(err)
	}
	return sources
}

func TestPoint_AppendsTheYouTubePointerAndLogsIt(t *testing.T) {
	s := testStore(t)
	track := seedBound(t, s, uuid.NewString(), `[{"provider": "soundcloud", "external_id": "77"}]`)
	runID := uuid.New()
	verdict, err := s.point(t.Context(), runID, track, "AbCdEfGhIj1")
	if err != nil || verdict != writeDone {
		t.Fatalf("verdict %q, err %v", verdict, err)
	}
	got := sourcesOf(t, s, track.TrackID)
	if !strings.Contains(got, `"external_id": "AbCdEfGhIj1"`) || !strings.Contains(got, `"provider": "youtube"`) || !strings.Contains(got, `"soundcloud"`) {
		t.Fatalf("catalog_sources = %s", got)
	}
	var logged int
	if err := s.pool.QueryRow(t.Context(), "SELECT count(*) FROM track_metadata_changes WHERE run_id = $1 AND track_id = $2 AND field = 'catalog_sources'", runID, track.TrackID).Scan(&logged); err != nil || logged != 1 {
		t.Fatalf("logged %d, err %v", logged, err)
	}
}

func TestPoint_NeverOverwritesAnExistingYouTubePointer(t *testing.T) {
	s := testStore(t)
	track := seedBound(t, s, uuid.NewString(), `[{"provider": "youtube", "external_id": "KeepKeepKe1"}]`)
	verdict, err := s.point(t.Context(), uuid.New(), track, "AbCdEfGhIj1")
	if err != nil || verdict != writeSkipped {
		t.Fatalf("verdict %q, err %v", verdict, err)
	}
	if got := sourcesOf(t, s, track.TrackID); !strings.Contains(got, "KeepKeepKe1") || strings.Contains(got, "AbCdEfGhIj1") {
		t.Fatalf("catalog_sources = %s", got)
	}
}

func TestPoint_SkipsATrackChangedSinceItWasRead(t *testing.T) {
	s := testStore(t)
	track := seedBound(t, s, uuid.NewString(), `[]`)
	if _, err := s.pool.Exec(t.Context(), "UPDATE tracks SET version = version + 1 WHERE id = $1", track.TrackID); err != nil {
		t.Fatal(err)
	}
	verdict, err := s.point(t.Context(), uuid.New(), track, "AbCdEfGhIj1")
	if err != nil || verdict != writeSkipped || sourcesOf(t, s, track.TrackID) != "[]" {
		t.Fatalf("verdict %q, err %v, sources %s", verdict, err, sourcesOf(t, s, track.TrackID))
	}
}

func TestLoadTargets_BoundReadyTracksWithoutAYouTubePointer(t *testing.T) {
	s := testStore(t)
	userID := uuid.NewString()
	open := seedBound(t, s, userID, `[{"provider": "soundcloud", "external_id": "77"}]`)
	seedBound(t, s, userID, `[{"provider": "youtube", "external_id": "KeepKeepKe1"}]`)
	unbound := seedBound(t, s, userID, `[]`)
	if _, err := s.pool.Exec(t.Context(), "UPDATE track_identity SET status = 'unbound' WHERE track_id = $1", unbound.TrackID); err != nil {
		t.Fatal(err)
	}
	targets, err := s.loadTargets(t.Context(), userID)
	if err != nil || len(targets) != 1 {
		t.Fatalf("targets %+v, err %v", targets, err)
	}
	if got := targets[0]; got.TrackID != open.TrackID || got.ISRC != open.ISRC || got.CatalogSeconds != 280 || got.Version != open.Version {
		t.Fatalf("got %+v", got)
	}
}

func TestUndo_RemovesWhatTheRunWroteWhereUnchanged(t *testing.T) {
	s := testStore(t)
	userID := uuid.NewString()
	untouched := seedBound(t, s, userID, `[{"provider": "soundcloud", "external_id": "77"}]`)
	edited := seedBound(t, s, userID, `[]`)
	runID := uuid.New()
	for _, track := range []target{untouched, edited} {
		if verdict, err := s.point(t.Context(), runID, track, "AbCdEfGhIj1"); err != nil || verdict != writeDone {
			t.Fatalf("verdict %q, err %v", verdict, err)
		}
	}
	if _, err := s.pool.Exec(t.Context(), `UPDATE tracks SET catalog_sources = '[{"provider": "soundcloud", "external_id": "9"}]' WHERE id = $1`, edited.TrackID); err != nil {
		t.Fatal(err)
	}
	tally, err := s.undo(t.Context(), runID)
	if err != nil || tally != (undoTally{Restored: 1, Kept: 1}) {
		t.Fatalf("tally %+v, err %v", tally, err)
	}
	if got := sourcesOf(t, s, untouched.TrackID); strings.Contains(got, "youtube") || !strings.Contains(got, "soundcloud") {
		t.Fatalf("catalog_sources = %s", got)
	}
	if got := sourcesOf(t, s, edited.TrackID); !strings.Contains(got, `"9"`) {
		t.Fatalf("an edited track was rewritten: %s", got)
	}
}

func TestUndo_AnUnknownRunHasNothingToUndo(t *testing.T) {
	s := testStore(t)
	if _, err := s.undo(t.Context(), uuid.New()); !errors.Is(err, errNothingToUndo) {
		t.Fatalf("err = %v", err)
	}
}

func TestPoint_OnAReadOnlyStoreIsRefusedByTheDatabaseWhateverTheSessionDefault(t *testing.T) {
	seeded := testStore(t)
	track := seedBound(t, seeded, uuid.NewString(), `[]`)
	dry := store{pool: seeded.pool, access: pgx.ReadOnly}

	verdict, err := dry.point(t.Context(), uuid.New(), track, "AbCdEfGhIj1")
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "25006" || verdict != writeSkipped {
		t.Fatalf("verdict %q, err %v, want SQLSTATE 25006", verdict, err)
	}
	if got := sourcesOf(t, seeded, track.TrackID); got != "[]" {
		t.Fatalf("catalog_sources = %s, want unchanged", got)
	}
}

func readOnlyTrapURL(t *testing.T, s store) string {
	t.Helper()
	schema := "probe_trap_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	stmts := []string{
		`CREATE SCHEMA ` + schema,
		`CREATE FUNCTION ` + schema + `.guard() RETURNS boolean LANGUAGE plpgsql VOLATILE AS $$
		BEGIN
			IF current_setting('transaction_read_only') <> 'on' THEN
				RAISE EXCEPTION 'probe-trap: table touched outside a read-only transaction';
			END IF;
			RETURN true;
		END $$`,
	}
	for _, table := range []string{"tracks", "track_identity", "track_metadata_changes"} {
		stmts = append(stmts, `CREATE VIEW `+schema+`.`+table+` AS SELECT * FROM public.`+table+` WHERE (SELECT `+schema+`.guard())`)
	}
	for _, stmt := range stmts {
		if _, err := s.pool.Exec(context.Background(), stmt); err != nil {
			t.Fatalf("trap: %v", err)
		}
	}
	t.Cleanup(func() { _, _ = s.pool.Exec(context.Background(), `DROP SCHEMA `+schema+` CASCADE`) })
	u, err := url.Parse(os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema+",public")
	u.RawQuery = q.Encode()
	return u.String()
}

func TestOpenPool_SetsNoReadOnlySessionDefault(t *testing.T) {
	testStore(t)
	pool, err := openPool(t.Context(), os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	var setting string
	if err := pool.QueryRow(t.Context(), `SHOW transaction_read_only`).Scan(&setting); err != nil {
		t.Fatal(err)
	}
	if setting == "on" {
		t.Fatalf("openPool left the session read-only by default: %q", setting)
	}
}

func envWithDatabase(t *testing.T, databaseURL string) func(string) string {
	t.Helper()
	values := map[string]string{"DATABASE_URL": databaseURL, "REDIS_URL": "redis://" + miniredis.RunT(t).Addr()}
	return func(name string) string { return values[name] }
}

func TestRun_DryRunTouchesTablesOnlyInsideReadOnlyTransactions(t *testing.T) {
	s := testStore(t)
	trap := readOnlyTrapURL(t, s)
	user := uuid.NewString()

	var out bytes.Buffer
	if err := run(options{userID: user}, envWithDatabase(t, trap), &out); err != nil {
		t.Fatalf("dry run: %v\n%s", err, out.String())
	}

	err := run(options{userID: user, apply: true}, envWithDatabase(t, trap), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "probe-trap") {
		t.Fatalf("an apply run read outside a read-write transaction should trip the trap, got %v", err)
	}
}

func TestRun_UndoStillRemovesTheRunsYouTubePointer(t *testing.T) {
	s := testStore(t)
	track := seedBound(t, s, uuid.NewString(), `[{"provider": "soundcloud", "external_id": "77"}]`)
	runID := uuid.New()
	if verdict, err := s.point(t.Context(), runID, track, "AbCdEfGhIj1"); err != nil || verdict != writeDone {
		t.Fatalf("verdict %q, err %v", verdict, err)
	}

	var out bytes.Buffer
	if err := run(options{undo: true, undoRunID: runID}, envWithDatabase(t, os.Getenv("DATABASE_URL")), &out); err != nil {
		t.Fatalf("undo: %v\n%s", err, out.String())
	}
	if got := sourcesOf(t, s, track.TrackID); strings.Contains(got, "youtube") || !strings.Contains(got, "soundcloud") {
		t.Fatalf("catalog_sources = %s", got)
	}
}

func TestStore_UndoOnAReadOnlyStoreIsRefusedByTheDatabase(t *testing.T) {
	seeded := testStore(t)
	track := seedBound(t, seeded, uuid.NewString(), `[]`)
	runID := uuid.New()
	if verdict, err := seeded.point(t.Context(), runID, track, "AbCdEfGhIj1"); err != nil || verdict != writeDone {
		t.Fatalf("verdict %q, err %v", verdict, err)
	}
	dry := store{pool: seeded.pool, access: pgx.ReadOnly}

	_, err := dry.undo(t.Context(), runID)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "25006" {
		t.Fatalf("err %v, want SQLSTATE 25006", err)
	}
	if got := sourcesOf(t, seeded, track.TrackID); !strings.Contains(got, "AbCdEfGhIj1") {
		t.Fatalf("a read-only undo removed the pointer: %s", got)
	}
}
