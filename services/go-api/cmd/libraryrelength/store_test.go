package main

import (
	"altune/go-api/internal/acquisition/adapters/persistence"
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

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
	return store{pool: pool, queue: persistence.NewPgxJobQueue(pool)}
}

type seeded struct {
	trackID string
	userID  string
}

func seedReady(t *testing.T, s store, isrc string, fileSeconds float64) seeded {
	t.Helper()
	row := seeded{trackID: uuid.NewString(), userID: uuid.NewString()}
	_, err := s.pool.Exec(context.Background(),
		`INSERT INTO tracks (id, user_id, title, artist, dedup_key, isrc, duration_seconds, acquisition_status, audio_ref, audio_version, version)
		 VALUES ($1, $2, 'Song', 'Artist', $5, $3, $4, 'ready', 'audio/old.mp3', 'v1', 4)`,
		row.trackID, row.userID, isrc, fileSeconds, row.trackID)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	t.Cleanup(func() {
		_, _ = s.pool.Exec(context.Background(), "DELETE FROM track_length_fixes WHERE track_id = $1", row.trackID)
		_, _ = s.pool.Exec(context.Background(), "DELETE FROM tracks WHERE id = $1", row.trackID)
	})
	return row
}

type trackState struct {
	isrc         *string
	duration     *float64
	catalog      *float64
	mismatch     bool
	version      int
	jobKind      string
	jobQueued    bool
	status       string
	loggedFixes  int
	audioVersion *string
}

func stateOf(t *testing.T, s store, row seeded) trackState {
	t.Helper()
	var got trackState
	err := s.pool.QueryRow(context.Background(),
		`SELECT isrc, duration_seconds, catalog_duration_seconds, audio_length_mismatch, version, acquisition_job_kind,
		        acquisition_available_at IS NOT NULL, acquisition_status, audio_version,
		        (SELECT count(*) FROM track_length_fixes WHERE track_id = t.id)
		 FROM tracks t WHERE id = $1`, row.trackID).
		Scan(&got.isrc, &got.duration, &got.catalog, &got.mismatch, &got.version, &got.jobKind, &got.jobQueued, &got.status, &got.audioVersion, &got.loggedFixes)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func changeFor(row seeded, act action, newISRC string, file, catalog float64) change {
	return change{
		TrackID: row.trackID, UserID: row.userID, Artist: "Artist", Title: "Song", Version: 4,
		Action: act, OldISRC: "USALBUM", NewISRC: newISRC, FileSeconds: file, CatalogSeconds: catalog,
	}
}

func TestStore_FixISRCChangesTheLabelAndKeepsTheAudioAndUndoRestoresIt(t *testing.T) {
	s := testStore(t)
	row := seedReady(t, s, "USALBUM", 226)
	runID := uuid.New()
	got, err := s.apply(context.Background(), runID, changeFor(row, actionFixISRC, "USRADIO", 226, 225), true)
	if err != nil || got != verdictApplied {
		t.Fatalf("got %s, %v", got, err)
	}
	after := stateOf(t, s, row)
	if *after.isrc != "USRADIO" || *after.catalog != 225 || *after.duration != 226 || after.mismatch || after.version != 5 || after.jobQueued || after.loggedFixes != 1 {
		t.Fatalf("after fix %+v", after)
	}
	results, err := s.undoRun(context.Background(), runID)
	if err != nil || len(results) != 1 || results[0].Verdict != undoRestored {
		t.Fatalf("undo %+v, %v", results, err)
	}
	undone := stateOf(t, s, row)
	if *undone.isrc != "USALBUM" || undone.catalog != nil || undone.version != 6 {
		t.Fatalf("after undo %+v", undone)
	}
	if _, err := s.undoRun(context.Background(), runID); err == nil {
		t.Fatal("a second undo must find nothing to undo")
	}
}

func TestStore_ReacquireKeepsTheFileLengthAndQueuesAReplaceJob(t *testing.T) {
	s := testStore(t)
	row := seedReady(t, s, "USALBUM", 226)
	got, err := s.apply(context.Background(), uuid.New(), changeFor(row, actionReacquire, "", 226, 379), true)
	if err != nil || got != verdictApplied {
		t.Fatalf("got %s, %v", got, err)
	}
	after := stateOf(t, s, row)
	if *after.catalog != 379 || *after.duration != 226 || !after.mismatch || after.jobKind != "replace" || !after.jobQueued ||
		after.status != "ready" || *after.audioVersion != "v1" || after.loggedFixes != 1 {
		t.Fatalf("after enqueue %+v", after)
	}
}

func TestStore_DryRunWritesNothing(t *testing.T) {
	s := testStore(t)
	row := seedReady(t, s, "USALBUM", 226)
	before := stateOf(t, s, row)
	for _, c := range []change{changeFor(row, actionReacquire, "", 226, 379), changeFor(row, actionFixISRC, "USRADIO", 226, 225)} {
		got, err := s.apply(context.Background(), uuid.New(), c, false)
		if err != nil || got != verdictApplied {
			t.Fatalf("got %s, %v", got, err)
		}
	}
	if after := stateOf(t, s, row); !reflect.DeepEqual(after, before) {
		t.Fatalf("dry run changed the track: %+v -> %+v", before, after)
	}
}

func TestStore_ReacquireSkipsATrackAlreadyEnqueuedOrInFlight(t *testing.T) {
	s := testStore(t)
	row := seedReady(t, s, "USALBUM", 226)
	c := changeFor(row, actionReacquire, "", 226, 379)
	if got, err := s.apply(context.Background(), uuid.New(), c, true); err != nil || got != verdictApplied {
		t.Fatalf("first: %s, %v", got, err)
	}
	c.Version = 5
	if got, err := s.apply(context.Background(), uuid.New(), c, true); err != nil || got != verdictInFlight {
		t.Fatalf("while queued: %s, %v", got, err)
	}
	if _, err := s.pool.Exec(context.Background(), "UPDATE tracks SET acquisition_available_at = NULL WHERE id = $1", row.trackID); err != nil {
		t.Fatal(err)
	}
	if got, err := s.apply(context.Background(), uuid.New(), c, true); err != nil || got != verdictAlreadyEnqueued {
		t.Fatalf("after settling: %s, %v", got, err)
	}
	if after := stateOf(t, s, row); after.loggedFixes != 1 {
		t.Fatalf("logged %d", after.loggedFixes)
	}
}

func TestStore_ApplyRefusesWhatChangedSinceTheAudit(t *testing.T) {
	s := testStore(t)
	row := seedReady(t, s, "USOTHER", 226)
	if got, _ := s.apply(context.Background(), uuid.New(), changeFor(row, actionFixISRC, "USRADIO", 226, 225), true); got != verdictISRCChanged {
		t.Fatalf("isrc moved: %s", got)
	}
	stale := changeFor(row, actionReacquire, "", 226, 379)
	stale.OldISRC, stale.Version = "USOTHER", 3
	if got, _ := s.apply(context.Background(), uuid.New(), stale, true); got != verdictStale {
		t.Fatalf("version moved: %s", got)
	}
	missing := changeFor(seeded{trackID: uuid.NewString(), userID: uuid.NewString()}, actionReacquire, "", 226, 379)
	if got, _ := s.apply(context.Background(), uuid.New(), missing, true); got != verdictStale {
		t.Fatalf("no such track: %s", got)
	}
	if after := stateOf(t, s, row); after.loggedFixes != 0 || after.version != 4 {
		t.Fatalf("refused changes wrote %+v", after)
	}
}

func TestStore_UndoOfAReacquireRestoresTheLengthsUntilTheAudioIsReplaced(t *testing.T) {
	s := testStore(t)
	row := seedReady(t, s, "USALBUM", 226)
	runID := uuid.New()
	if got, err := s.apply(context.Background(), runID, changeFor(row, actionReacquire, "", 226, 379), true); err != nil || got != verdictApplied {
		t.Fatalf("apply: %s, %v", got, err)
	}
	if results, _ := s.undoRun(context.Background(), runID); results[0].Verdict != undoSkippedInFlight {
		t.Fatalf("while queued: %+v", results)
	}
	if _, err := s.pool.Exec(context.Background(), "UPDATE tracks SET acquisition_available_at = NULL WHERE id = $1", row.trackID); err != nil {
		t.Fatal(err)
	}
	if results, _ := s.undoRun(context.Background(), runID); results[0].Verdict != undoRestored {
		t.Fatalf("after a failed replace: %+v", results)
	}
	if undone := stateOf(t, s, row); *undone.duration != 226 || undone.catalog != nil || undone.mismatch {
		t.Fatalf("after undo %+v", undone)
	}
}

func TestStore_UndoRefusesAReplacedTrack(t *testing.T) {
	s := testStore(t)
	row := seedReady(t, s, "USALBUM", 226)
	runID := uuid.New()
	if got, err := s.apply(context.Background(), runID, changeFor(row, actionReacquire, "", 226, 379), true); err != nil || got != verdictApplied {
		t.Fatalf("apply: %s, %v", got, err)
	}
	if _, err := s.pool.Exec(context.Background(), "UPDATE tracks SET acquisition_available_at = NULL, audio_version = 'v2', duration_seconds = 380 WHERE id = $1", row.trackID); err != nil {
		t.Fatal(err)
	}
	if results, _ := s.undoRun(context.Background(), runID); results[0].Verdict != undoSkippedReplaced {
		t.Fatalf("got %+v", results)
	}
	if kept := stateOf(t, s, row); *kept.duration != 380 || *kept.catalog != 379 {
		t.Fatalf("a replaced track must keep its new lengths: %+v", kept)
	}
}

type failingQueue struct{}

func (failingQueue) Enqueue(context.Context, shared.TrackId, ports.JobKind, time.Duration) error {
	return errors.New("queue down")
}

func TestStore_ReacquireThatCannotEnqueueRestoresTheTrack(t *testing.T) {
	s := testStore(t)
	s.queue = failingQueue{}
	row := seedReady(t, s, "USALBUM", 226)
	got, err := s.apply(context.Background(), uuid.New(), changeFor(row, actionReacquire, "", 226, 379), true)
	if got != verdictEnqueueFailed || err == nil {
		t.Fatalf("got %s, %v", got, err)
	}
	if after := stateOf(t, s, row); *after.duration != 226 || after.catalog != nil || after.mismatch || after.jobQueued {
		t.Fatalf("after failed enqueue %+v", after)
	}
}

type cancellingQueue struct{ cancel context.CancelFunc }

func (q cancellingQueue) Enqueue(context.Context, shared.TrackId, ports.JobKind, time.Duration) error {
	q.cancel()
	return errors.New("queue down")
}

func TestStore_RestoreAfterAFailedEnqueueSurvivesTheRunContextEnding(t *testing.T) {
	s := testStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.queue = cancellingQueue{cancel: cancel}
	row := seedReady(t, s, "USALBUM", 226)
	got, err := s.apply(ctx, uuid.New(), changeFor(row, actionReacquire, "", 226, 379), true)
	if got != verdictEnqueueFailed || err == nil || strings.Contains(err.Error(), "restoring track") {
		t.Fatalf("got %s, %v", got, err)
	}
	if after := stateOf(t, s, row); *after.duration != 226 || after.catalog != nil || after.mismatch || after.jobQueued {
		t.Fatalf("after failed enqueue %+v", after)
	}
}
