package main

import (
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
