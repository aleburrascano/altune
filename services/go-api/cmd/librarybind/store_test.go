package main

import (
	"context"
	"errors"
	"os"
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

func seedReady(t *testing.T, s store, userID string) libraryTrack {
	t.Helper()
	track := celebration()
	track.ID, track.UserID, track.Version = uuid.NewString(), userID, 4
	_, err := s.pool.Exec(context.Background(),
		`INSERT INTO tracks (id, user_id, title, artist, album, dedup_key, isrc, duration_seconds, acquisition_status, audio_ref, audio_version, version)
		 VALUES ($1, $2, $3, $4, $5, $9, $6, $7, 'ready', 'audio/a.mp3', 'v1', $8)`,
		track.ID, track.UserID, track.Title, track.Artist, track.Album, track.ISRC, track.FileSeconds, track.Version, track.ID)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	t.Cleanup(func() { _, _ = s.pool.Exec(context.Background(), "DELETE FROM tracks WHERE id = $1", track.ID) })
	return track
}

type identityRow struct {
	status   status
	reason   string
	deezerID *string
	apple    *string
	mbID     *string
	isrc     *string
	proof    string
	version  int
	runID    uuid.UUID
}

func identityOf(t *testing.T, s store, trackID string) (identityRow, bool) {
	t.Helper()
	var row identityRow
	var raw string
	err := s.pool.QueryRow(context.Background(),
		`SELECT status, reason, deezer_track_id, apple_song_id, mb_recording_id, isrc, evidence::text, track_version_at_bind, run_id
		 FROM track_identity WHERE track_id = $1`, trackID).
		Scan(&row.status, &row.reason, &row.deezerID, &row.apple, &row.mbID, &row.isrc, &raw, &row.version, &row.runID)
	if err != nil {
		return identityRow{}, false
	}
	row.proof = raw
	return row, true
}

func boundIdentity(t libraryTrack) identity {
	return decide(t, []sourceOutcome{outcomeOf(t, sourceDeezer, []candidate{catalogCelebration("77")}, nil)})
}

func unboundIdentity(t libraryTrack) identity {
	return decide(t, []sourceOutcome{outcomeOf(t, sourceDeezer, nil, nil)})
}

func trackVersion(t *testing.T, s store, trackID string) int {
	t.Helper()
	var version int
	if err := s.pool.QueryRow(context.Background(), "SELECT version FROM tracks WHERE id = $1", trackID).Scan(&version); err != nil {
		t.Fatal(err)
	}
	return version
}

func TestStore_SaveWritesTheIdentityAndLeavesTheTrackAlone(t *testing.T) {
	s := testStore(t)
	track := seedReady(t, s, uuid.NewString())
	runID := uuid.New()

	verdict, err := s.save(t.Context(), runID, track, boundIdentity(track))
	if err != nil || verdict != saveWritten {
		t.Fatalf("save: %v, %v", verdict, err)
	}
	row, ok := identityOf(t, s, track.ID)
	if !ok || row.status != statusBound || row.deezerID == nil || *row.deezerID != "77" || row.apple != nil || row.mbID != nil {
		t.Fatalf("row = %+v, found %v", row, ok)
	}
	if row.version != track.Version || row.runID != runID || row.isrc == nil || *row.isrc != celebrationISRC {
		t.Errorf("row = %+v", row)
	}
	if row.proof == "" || !containsAll(row.proof, `"file_seconds": 298`, `"winner": "77"`, `"artists": ["Kool & The Gang"]`) {
		t.Errorf("evidence = %s", row.proof)
	}
	if got := trackVersion(t, s, track.ID); got != track.Version {
		t.Errorf("track version = %d, want unchanged %d", got, track.Version)
	}
}

func containsAll(text string, parts ...string) bool {
	for _, part := range parts {
		if !contains(text, part) {
			return false
		}
	}
	return true
}

func contains(text, part string) bool {
	for i := 0; i+len(part) <= len(text); i++ {
		if text[i:i+len(part)] == part {
			return true
		}
	}
	return false
}

func TestStore_LoadsTheAudioSourceURLAndSavesAnUncataloguedUploadAsUnofficialWithItsSourceKey(t *testing.T) {
	s := testStore(t)
	user := uuid.NewString()
	track := seedReady(t, s, user)
	const upload = "https://soundcloud.com/curre-2/chicago-finale-mp3"
	if _, err := s.pool.Exec(t.Context(), "UPDATE tracks SET audio_source_url = $2 WHERE id = $1", track.ID, upload); err != nil {
		t.Fatal(err)
	}
	loaded, err := s.loadTracks(t.Context(), user, 0)
	if err != nil || len(loaded) != 1 || loaded[0].SourceURL != upload {
		t.Fatalf("loaded %+v, %v", loaded, err)
	}
	found, err := markUncatalogued(t.Context(), loaded[0], answeredWithNothing(reasonISRCNotFound))
	if err != nil {
		t.Fatal(err)
	}
	if verdict, err := s.save(t.Context(), uuid.New(), loaded[0], found); err != nil || verdict != saveWritten {
		t.Fatalf("save: %v, %v", verdict, err)
	}
	row, ok := identityOf(t, s, track.ID)
	if !ok || row.status != statusUnofficial || row.reason != reasonNotInCatalog || !contains(row.proof, `"source_key": "soundcloud.com/curre-2/chicago-finale-mp3"`) {
		t.Fatalf("row = %+v, found %v", row, ok)
	}
}

func TestStore_SaveSkipsATrackThatChangedSinceItWasRead(t *testing.T) {
	s := testStore(t)
	track := seedReady(t, s, uuid.NewString())
	if _, err := s.pool.Exec(t.Context(), "UPDATE tracks SET version = version + 1 WHERE id = $1", track.ID); err != nil {
		t.Fatal(err)
	}
	verdict, err := s.save(t.Context(), uuid.New(), track, boundIdentity(track))
	if err != nil || verdict != saveSkipped {
		t.Fatalf("save: %v, %v", verdict, err)
	}
	if _, ok := identityOf(t, s, track.ID); ok {
		t.Fatal("an identity was written for a stale read")
	}
}

func TestStore_SaveReplacesAnUnboundRowButNeverABoundOne(t *testing.T) {
	s := testStore(t)
	track := seedReady(t, s, uuid.NewString())
	first, second, third := uuid.New(), uuid.New(), uuid.New()

	if verdict, err := s.save(t.Context(), first, track, unboundIdentity(track)); err != nil || verdict != saveWritten {
		t.Fatalf("unbound: %v, %v", verdict, err)
	}
	if verdict, err := s.save(t.Context(), second, track, boundIdentity(track)); err != nil || verdict != saveWritten {
		t.Fatalf("rebind: %v, %v", verdict, err)
	}
	if row, _ := identityOf(t, s, track.ID); row.status != statusBound || row.runID != second {
		t.Fatalf("row = %+v", row)
	}
	if verdict, err := s.save(t.Context(), third, track, unboundIdentity(track)); err != nil || verdict != saveSkipped {
		t.Fatalf("overwrite bound: %v, %v", verdict, err)
	}
	if row, _ := identityOf(t, s, track.ID); row.status != statusBound || row.runID != second {
		t.Fatalf("bound row was overwritten: %+v", row)
	}
}

func TestStore_LoadTracksSkipsBoundAndUnofficialButKeepsUnbound(t *testing.T) {
	s := testStore(t)
	user := uuid.NewString()
	bound, unbound, fresh := seedReady(t, s, user), seedReady(t, s, user), seedReady(t, s, user)
	unofficial := seedReady(t, s, user)
	unofficial.Album = "UNRELEASED"
	if _, err := s.save(t.Context(), uuid.New(), bound, boundIdentity(bound)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.save(t.Context(), uuid.New(), unbound, unboundIdentity(unbound)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.save(t.Context(), uuid.New(), unofficial, decide(unofficial, nil)); err != nil {
		t.Fatal(err)
	}

	loaded, err := s.loadTracks(t.Context(), user, 0)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, tr := range loaded {
		ids[tr.ID] = true
	}
	if len(loaded) != 2 || !ids[unbound.ID] || !ids[fresh.ID] {
		t.Fatalf("loaded = %+v", loaded)
	}
	limited, err := s.loadTracks(t.Context(), user, 1)
	if err != nil || len(limited) != 1 {
		t.Fatalf("limit: %+v, %v", limited, err)
	}
}

func TestStore_UndoRunRemovesUnchangedRowsAndKeepsRowsWhoseTrackMoved(t *testing.T) {
	s := testStore(t)
	user := uuid.NewString()
	untouched, moved, otherRun := seedReady(t, s, user), seedReady(t, s, user), seedReady(t, s, user)
	runID, otherRunID := uuid.New(), uuid.New()
	for _, track := range []libraryTrack{untouched, moved} {
		if _, err := s.save(t.Context(), runID, track, boundIdentity(track)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.save(t.Context(), otherRunID, otherRun, boundIdentity(otherRun)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(t.Context(), "UPDATE tracks SET version = version + 1 WHERE id = $1", moved.ID); err != nil {
		t.Fatal(err)
	}

	tally, err := s.undoRun(t.Context(), runID)
	if err != nil || tally.Removed != 1 || tally.Kept != 1 {
		t.Fatalf("undo: %+v, %v", tally, err)
	}
	if _, ok := identityOf(t, s, untouched.ID); ok {
		t.Error("the unchanged track kept its identity")
	}
	if _, ok := identityOf(t, s, moved.ID); !ok {
		t.Error("the moved track lost its identity")
	}
	if _, ok := identityOf(t, s, otherRun.ID); !ok {
		t.Error("another run's identity was removed")
	}
}

func TestStore_UndoRunWithoutRowsIsAnError(t *testing.T) {
	s := testStore(t)
	if _, err := s.undoRun(t.Context(), uuid.New()); !errors.Is(err, errNothingToUndo) {
		t.Fatalf("err = %v", err)
	}
}

func TestStore_DeletingATrackDeletesItsIdentity(t *testing.T) {
	s := testStore(t)
	track := seedReady(t, s, uuid.NewString())
	if _, err := s.save(t.Context(), uuid.New(), track, boundIdentity(track)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(t.Context(), "DELETE FROM tracks WHERE id = $1", track.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := identityOf(t, s, track.ID); ok {
		t.Fatal("identity outlived its track")
	}
}
