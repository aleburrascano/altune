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

func TestStore_LoadPinTargetsReportsVersionAndIdentityStatusPerTrack(t *testing.T) {
	s := testStore(t)
	user := uuid.NewString()
	fresh, unbound, bound := seedReady(t, s, user), seedReady(t, s, user), seedReady(t, s, user)
	if _, err := s.save(t.Context(), uuid.New(), unbound, unboundIdentity(unbound)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.save(t.Context(), uuid.New(), bound, boundIdentity(bound)); err != nil {
		t.Fatal(err)
	}

	targets, err := s.loadPinTargets(t.Context(), []string{fresh.ID, unbound.ID, bound.ID, uuid.NewString()})
	if err != nil || len(targets) != 3 {
		t.Fatalf("targets = %+v, %v", targets, err)
	}
	if got := targets[fresh.ID]; got.IdentityStatus != "" || got.Track.Version != fresh.Version || got.Track.Title != fresh.Title {
		t.Errorf("fresh = %+v", got)
	}
	if targets[unbound.ID].IdentityStatus != statusUnbound || targets[bound.ID].IdentityStatus != statusBound {
		t.Errorf("targets = %+v", targets)
	}
}

func TestStore_PinsFillAnUnboundTrackKeepABoundOneAndUndoWithTheirRun(t *testing.T) {
	s := testStore(t)
	user := uuid.NewString()
	toPin, alreadyBound := seedReady(t, s, user), seedReady(t, s, user)
	if _, err := s.save(t.Context(), uuid.New(), toPin, unboundIdentity(toPin)); err != nil {
		t.Fatal(err)
	}
	boundRun := uuid.New()
	if _, err := s.save(t.Context(), boundRun, alreadyBound, boundIdentity(alreadyBound)); err != nil {
		t.Fatal(err)
	}
	pinsRaw := []byte(`[
		{"track_id":"` + toPin.ID + `","track_version":4,"status":"bound","isrc":"USPR18000001","deezer_track_id":"77","note":"hand check","evidence":{"url":"https://example.test/1"}},
		{"track_id":"` + alreadyBound.ID + `","track_version":4,"status":"unofficial","note":"should not land"}
	]`)
	runID := uuid.New()
	report := newReport()
	if err := runPins(t.Context(), pinsRaw, s.loadPinTargets, s, runID, report); err != nil {
		t.Fatalf("runPins: %v\n%s", err, report.String())
	}

	row, ok := identityOf(t, s, toPin.ID)
	if !ok || row.status != statusBound || row.reason != "pinned: hand check" || row.runID != runID || row.deezerID == nil || *row.deezerID != "77" {
		t.Fatalf("pinned row = %+v, found %v", row, ok)
	}
	if !containsAll(row.proof, `"pin": {"url": "https://example.test/1"}`, `"title": "Celebration"`) {
		t.Errorf("evidence = %s", row.proof)
	}
	if kept, _ := identityOf(t, s, alreadyBound.ID); kept.status != statusBound || kept.runID != boundRun {
		t.Errorf("a bound row was overwritten: %+v", kept)
	}

	if tally, err := s.undoRun(t.Context(), runID); err != nil || tally.Removed != 1 {
		t.Fatalf("undo: %+v, %v", tally, err)
	}
	if _, ok := identityOf(t, s, toPin.ID); ok {
		t.Error("the pinned row survived its undo")
	}
}

func TestStore_SavePinsTheBoundRecordingAsCatalogMBIDAndUndoRestoresNull(t *testing.T) {
	s := testStore(t)
	track := seedReady(t, s, uuid.NewString())
	recording, runID := uuid.NewString(), uuid.New()

	if verdict, err := s.save(t.Context(), runID, track, boundToRecording(track, recording)); err != nil || verdict != saveWritten {
		t.Fatalf("save: %v, %v", verdict, err)
	}
	if pin := catalogMBIDOf(t, s, track.ID); pin == nil || *pin != recording {
		t.Fatalf("catalog_mbid = %v, want %s", pin, recording)
	}
	if got := trackVersion(t, s, track.ID); got != track.Version {
		t.Errorf("track version = %d, want unchanged %d", got, track.Version)
	}

	tally, err := s.undoRun(t.Context(), runID)
	if err != nil || tally.Removed != 1 || tally.Unpinned != 1 {
		t.Fatalf("undo: %+v, %v", tally, err)
	}
	if pin := catalogMBIDOf(t, s, track.ID); pin != nil {
		t.Errorf("catalog_mbid after undo = %s, want NULL", *pin)
	}
}

func TestStore_SaveNeverOverwritesAnotherCatalogMBIDNorPinsAnUnboundTrack(t *testing.T) {
	s := testStore(t)
	user, other := uuid.NewString(), uuid.NewString()
	pinned, unbound := seedReady(t, s, user), seedReady(t, s, user)
	setCatalogMBID(t, s, pinned.ID, &other)

	if _, err := s.save(t.Context(), uuid.New(), pinned, boundToRecording(pinned, uuid.NewString())); err != nil {
		t.Fatal(err)
	}
	if _, err := s.save(t.Context(), uuid.New(), unbound, unboundIdentity(unbound)); err != nil {
		t.Fatal(err)
	}

	if pin := catalogMBIDOf(t, s, pinned.ID); pin == nil || *pin != other {
		t.Errorf("catalog_mbid = %v, want untouched %s", pin, other)
	}
	if pin := catalogMBIDOf(t, s, unbound.ID); pin != nil {
		t.Errorf("an unbound track was pinned to %s", *pin)
	}
}

const (
	replacementMBID = "5c1d9d0e-7a3b-4f60-9e11-2b3c4d5e6f70"
	replacementISRC = "USUM71900002"
)

func replacePinJSON(trackID, replacesJSON string) []byte {
	return []byte(`[{"track_id":"` + trackID + `","track_version":4,"status":"bound","isrc":"` + replacementISRC + `","mb_recording_id":"` + replacementMBID +
		`","note":"studio release, not the live cut","evidence":{"url":"https://example.test/studio"},"replaces":` + replacesJSON + `}]`)
}

func boundWithOldIdentity(t *testing.T, s store) (libraryTrack, uuid.UUID) {
	t.Helper()
	track := seedReady(t, s, uuid.NewString())
	oldRun := uuid.New()
	if _, err := s.save(t.Context(), oldRun, track, boundIdentity(track)); err != nil {
		t.Fatal(err)
	}
	return track, oldRun
}

func TestStore_APinReplacesABoundIdentityWhenItsReplacesMatchesTheStoredRow(t *testing.T) {
	s := testStore(t)
	track, _ := boundWithOldIdentity(t, s)
	runID := uuid.New()
	report := newReport()

	raw := replacePinJSON(track.ID, `{"isrc":"`+celebrationISRC+`"}`)
	if err := runPins(t.Context(), raw, s.loadPinTargets, s, runID, report); err != nil {
		t.Fatalf("runPins: %v\n%s", err, report.String())
	}
	row, _ := identityOf(t, s, track.ID)
	if row.runID != runID || row.isrc == nil || *row.isrc != replacementISRC || row.mbID == nil || *row.mbID != replacementMBID || row.deezerID != nil {
		t.Fatalf("row = %+v", row)
	}
	if row.reason != "pinned: studio release, not the live cut" || !containsAll(row.proof, `"previous_identity"`, `"deezer_track_id": "77"`, `"url": "https://example.test/studio"`) {
		t.Errorf("reason %q, evidence %s", row.reason, row.proof)
	}
	if !contains(report.String(), "pinned: 1, skipped: 0") {
		t.Errorf("report = %s", report.String())
	}
}

func TestStore_APinWhoseReplacesDiffersFromTheStoredRowIsSkippedAndReported(t *testing.T) {
	s := testStore(t)
	track, oldRun := boundWithOldIdentity(t, s)
	report := newReport()

	raw := replacePinJSON(track.ID, `{"isrc":"USXX00000009"}`)
	if err := runPins(t.Context(), raw, s.loadPinTargets, s, uuid.New(), report); err != nil {
		t.Fatalf("runPins: %v\n%s", err, report.String())
	}
	if row, _ := identityOf(t, s, track.ID); row.runID != oldRun || row.deezerID == nil || *row.deezerID != "77" {
		t.Errorf("a mismatched replace overwrote the row: %+v", row)
	}
	if !contains(report.String(), "replaces-mismatch") || !contains(report.String(), "pinned: 0, skipped: 1") {
		t.Errorf("report = %s", report.String())
	}
}

func TestStore_TheReplaceGuardHoldsInSQLWhenTheRowChangedAfterThePinWasDecided(t *testing.T) {
	s := testStore(t)
	track, oldRun := boundWithOldIdentity(t, s)
	if _, err := s.pool.Exec(t.Context(), "UPDATE track_identity SET isrc = 'USXX00000009' WHERE track_id = $1", track.ID); err != nil {
		t.Fatal(err)
	}
	found := boundIdentity(track)
	found.Replaces = &replaces{ISRC: celebrationISRC}

	if verdict, err := s.save(t.Context(), uuid.New(), track, found); err != nil || verdict != saveSkipped {
		t.Fatalf("save: %v, %v", verdict, err)
	}
	if row, _ := identityOf(t, s, track.ID); row.runID != oldRun {
		t.Errorf("row = %+v", row)
	}
}

func TestStore_APinWithoutReplacesStillSkipsABoundRow(t *testing.T) {
	s := testStore(t)
	track, oldRun := boundWithOldIdentity(t, s)
	report := newReport()

	raw := []byte(`[{"track_id":"` + track.ID + `","track_version":4,"status":"bound","isrc":"` + replacementISRC + `","note":"no replaces","evidence":{"url":"https://example.test/x"}}]`)
	if err := runPins(t.Context(), raw, s.loadPinTargets, s, uuid.New(), report); err != nil {
		t.Fatalf("runPins: %v\n%s", err, report.String())
	}
	if row, _ := identityOf(t, s, track.ID); row.runID != oldRun || row.deezerID == nil || *row.deezerID != "77" {
		t.Errorf("row = %+v", row)
	}
	if !contains(report.String(), "already-identified") {
		t.Errorf("report = %s", report.String())
	}
}

func TestStore_UndoRestoresTheIdentityAReplacementOverwrote(t *testing.T) {
	s := testStore(t)
	track, oldRun := boundWithOldIdentity(t, s)
	before, _ := identityOf(t, s, track.ID)
	runID := uuid.New()
	raw := replacePinJSON(track.ID, `{"isrc":"`+celebrationISRC+`"}`)
	if err := runPins(t.Context(), raw, s.loadPinTargets, s, runID, newReport()); err != nil {
		t.Fatal(err)
	}

	tally, err := s.undoRun(t.Context(), runID)
	if err != nil || tally.Restored != 1 || tally.Removed != 0 || tally.Kept != 0 {
		t.Fatalf("undo: %+v, %v", tally, err)
	}
	after, ok := identityOf(t, s, track.ID)
	if !ok || after.runID != oldRun || after.status != before.status || after.reason != before.reason || after.proof != before.proof ||
		after.deezerID == nil || *after.deezerID != "77" || after.isrc == nil || *after.isrc != celebrationISRC || after.mbID != nil || after.version != before.version {
		t.Fatalf("after = %+v, want %+v", after, before)
	}
}

func replaceBoundToRecording(t *testing.T, s store, oldRecording string, catalog *string) (libraryTrack, uuid.UUID) {
	t.Helper()
	track := seedReady(t, s, uuid.NewString())
	if _, err := s.save(t.Context(), uuid.New(), track, boundToRecording(track, oldRecording)); err != nil {
		t.Fatal(err)
	}
	setCatalogMBID(t, s, track.ID, catalog)
	runID := uuid.New()
	raw := replacePinJSON(track.ID, `{"mb_recording_id":"`+oldRecording+`"}`)
	if err := runPins(t.Context(), raw, s.loadPinTargets, s, runID, newReport()); err != nil {
		t.Fatal(err)
	}
	return track, runID
}

func loggedCatalogChange(t *testing.T, s store, runID uuid.UUID, trackID string) (oldValue, newValue *string) {
	t.Helper()
	err := s.pool.QueryRow(t.Context(),
		"SELECT old_value, new_value FROM track_metadata_changes WHERE run_id = $1 AND track_id = $2 AND field = 'catalog_mbid'", runID, trackID).
		Scan(&oldValue, &newValue)
	if err != nil {
		t.Fatalf("catalog_mbid change not logged: %v", err)
	}
	return oldValue, newValue
}

func TestStore_AReplaceRepointsCatalogMBIDFromTheOldRecordingAndUndoPutsItBack(t *testing.T) {
	s := testStore(t)
	oldRecording := uuid.NewString()
	track, runID := replaceBoundToRecording(t, s, oldRecording, &oldRecording)

	if pin := catalogMBIDOf(t, s, track.ID); pin == nil || *pin != replacementMBID {
		t.Fatalf("catalog_mbid = %v, want %s", pin, replacementMBID)
	}
	if was, now := loggedCatalogChange(t, s, runID, track.ID); was == nil || *was != oldRecording || now == nil || *now != replacementMBID {
		t.Errorf("logged change = %v -> %v", was, now)
	}
	if got := trackVersion(t, s, track.ID); got != track.Version {
		t.Errorf("track version = %d, want unchanged %d", got, track.Version)
	}

	tally, err := s.undoRun(t.Context(), runID)
	if err != nil || tally.Restored != 1 || tally.Unpinned != 1 {
		t.Fatalf("undo: %+v, %v", tally, err)
	}
	if pin := catalogMBIDOf(t, s, track.ID); pin == nil || *pin != oldRecording {
		t.Errorf("catalog_mbid after undo = %v, want %s", pin, oldRecording)
	}
}

func TestStore_AReplaceFillsANullCatalogMBIDAndUndoRestoresNull(t *testing.T) {
	s := testStore(t)
	track, runID := replaceBoundToRecording(t, s, uuid.NewString(), nil)

	if pin := catalogMBIDOf(t, s, track.ID); pin == nil || *pin != replacementMBID {
		t.Fatalf("catalog_mbid = %v, want %s", pin, replacementMBID)
	}
	if _, err := s.undoRun(t.Context(), runID); err != nil {
		t.Fatal(err)
	}
	if pin := catalogMBIDOf(t, s, track.ID); pin != nil {
		t.Errorf("catalog_mbid after undo = %s, want NULL", *pin)
	}
}

func TestStore_AReplaceLeavesACatalogMBIDThatPointsElsewhere(t *testing.T) {
	s := testStore(t)
	elsewhere := uuid.NewString()
	track, runID := replaceBoundToRecording(t, s, uuid.NewString(), &elsewhere)

	if pin := catalogMBIDOf(t, s, track.ID); pin == nil || *pin != elsewhere {
		t.Errorf("catalog_mbid = %v, want untouched %s", pin, elsewhere)
	}
	if row, _ := identityOf(t, s, track.ID); row.runID != runID {
		t.Errorf("the identity was not replaced: %+v", row)
	}
}

func TestStore_UndoLeavesACatalogMBIDThatChangedAfterTheReplace(t *testing.T) {
	s := testStore(t)
	oldRecording, later := uuid.NewString(), uuid.NewString()
	track, runID := replaceBoundToRecording(t, s, oldRecording, &oldRecording)
	setCatalogMBID(t, s, track.ID, &later)

	tally, err := s.undoRun(t.Context(), runID)
	if err != nil || tally.Unpinned != 0 || tally.PinsKept != 1 {
		t.Fatalf("undo: %+v, %v", tally, err)
	}
	if pin := catalogMBIDOf(t, s, track.ID); pin == nil || *pin != later {
		t.Errorf("catalog_mbid after undo = %v, want %s", pin, later)
	}
}

func TestStore_AReplaceWithoutARecordingIdClearsACatalogMBIDOnTheReplacedRecordingAndUndoRestoresIt(t *testing.T) {
	s := testStore(t)
	oldRecording := uuid.NewString()
	track := seedReady(t, s, uuid.NewString())
	if _, err := s.save(t.Context(), uuid.New(), track, boundToRecording(track, oldRecording)); err != nil {
		t.Fatal(err)
	}
	runID := uuid.New()
	raw := []byte(`[{"track_id":"` + track.ID + `","track_version":4,"status":"bound","isrc":"` + replacementISRC +
		`","note":"studio release, no MB id","evidence":{"url":"https://example.test/studio"},"replaces":{"mb_recording_id":"` + oldRecording + `"}}]`)
	if err := runPins(t.Context(), raw, s.loadPinTargets, s, runID, newReport()); err != nil {
		t.Fatal(err)
	}

	if pin := catalogMBIDOf(t, s, track.ID); pin != nil {
		t.Fatalf("catalog_mbid = %s, want NULL", *pin)
	}
	if was, now := loggedCatalogChange(t, s, runID, track.ID); was == nil || *was != oldRecording || now != nil {
		t.Errorf("logged change = %v -> %v", was, now)
	}
	if tally, err := s.undoRun(t.Context(), runID); err != nil || tally.Unpinned != 1 || tally.Restored != 1 {
		t.Fatalf("undo: %+v, %v", tally, err)
	}
	if pin := catalogMBIDOf(t, s, track.ID); pin == nil || *pin != oldRecording {
		t.Errorf("catalog_mbid after undo = %v, want %s", pin, oldRecording)
	}
}
