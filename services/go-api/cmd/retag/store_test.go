package main

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTrackStore_ReadOnlyAccessRefusesAWriteOnAPlainPool(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	s := trackStore{pool: pool, access: pgx.ReadOnly}
	err = s.inTx(context.Background(), func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), "CREATE TABLE retag_readonly_probe (id int)")
		return err
	})
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "25006" {
		_, _ = pool.Exec(context.Background(), "DROP TABLE IF EXISTS retag_readonly_probe")
		t.Fatalf("got %v; want SQLSTATE 25006", err)
	}
}

func probePool(t *testing.T) *pgxpool.Pool {
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
	return pool
}

func seedReadyTrack(t *testing.T, pool *pgxpool.Pool, title string) (id, userID, ref string) {
	t.Helper()
	id, userID = uuid.NewString(), uuid.NewString()
	ref = "audio/" + id + ".mp3"
	_, err := pool.Exec(context.Background(),
		`INSERT INTO tracks (id, user_id, title, artist, dedup_key, duration_seconds, acquisition_status, audio_ref)
		 VALUES ($1, $2, $3, 'Artist', $5, 200, 'ready', $4)`, id, userID, title, ref, id)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM tracks WHERE id = $1", id) })
	return id, userID, ref
}

func titleOf(t *testing.T, pool *pgxpool.Pool, id string) string {
	t.Helper()
	var title string
	if err := pool.QueryRow(context.Background(), "SELECT title FROM tracks WHERE id = $1", id).Scan(&title); err != nil {
		t.Fatal(err)
	}
	return title
}

func TestTrackStore_ReadOnlyAccessRefusesAnUpdateToARealTable(t *testing.T) {
	pool := probePool(t)
	id, _, _ := seedReadyTrack(t, pool, "Before")
	s := trackStore{pool: pool, access: pgx.ReadOnly}
	err := s.inTx(context.Background(), func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), "UPDATE tracks SET title = 'After' WHERE id = $1", id)
		return err
	})
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "25006" {
		t.Fatalf("got %v; want SQLSTATE 25006", err)
	}
	if got := titleOf(t, pool, id); got != "Before" {
		t.Fatalf("read-only tx changed the title to %q", got)
	}
}

func TestTrackStore_ReadWriteAccessCommitsTheWrite(t *testing.T) {
	pool := probePool(t)
	id, _, _ := seedReadyTrack(t, pool, "Before")
	s := trackStore{pool: pool}
	err := s.inTx(context.Background(), func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), "UPDATE tracks SET title = 'After' WHERE id = $1", id)
		return err
	})
	if err != nil {
		t.Fatalf("read-write tx: %v", err)
	}
	if got := titleOf(t, pool, id); got != "After" {
		t.Fatalf("read-write tx did not commit: title %q", got)
	}
}

func TestTrackStore_DryRunReadsStillWorkWhenReadOnly(t *testing.T) {
	pool := probePool(t)
	id, userID, ref := seedReadyTrack(t, pool, "Probe Song")
	s := trackStore{pool: pool, access: pgx.ReadOnly}
	tracks, err := s.readyTracks(context.Background(), selection{})
	if err != nil {
		t.Fatalf("readyTracks on a read-only store: %v", err)
	}
	var found *trackRow
	for i := range tracks {
		if tracks[i].ID == id {
			found = &tracks[i]
		}
	}
	if found == nil || found.UserID != userID || found.Ref != ref || found.Tags.Title != "Probe Song" || found.Tags.Artist != "Artist" {
		t.Fatalf("readyTracks lost the seeded track: %+v", found)
	}
	serves, err := s.stillServes(context.Background(), *found)
	if err != nil || !serves {
		t.Fatalf("stillServes on the seeded ref: %v, %v", serves, err)
	}
	moved := *found
	moved.Ref = "audio/elsewhere.mp3"
	if serves, err := s.stillServes(context.Background(), moved); err != nil || serves {
		t.Fatalf("stillServes on another ref: %v, %v", serves, err)
	}
}
