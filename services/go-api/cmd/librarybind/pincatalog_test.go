package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func boundToRecording(track libraryTrack, recordingID string) identity {
	return decide(track, []sourceOutcome{
		outcomeOf(track, sourceDeezer, []candidate{catalogCelebration("77")}, nil),
		outcomeOf(track, sourceMusicBrainz, []candidate{catalogCelebration(recordingID)}, nil),
	})
}

func catalogMBIDOf(t *testing.T, s store, trackID string) *string {
	t.Helper()
	var pin *string
	if err := s.pool.QueryRow(context.Background(), "SELECT catalog_mbid FROM tracks WHERE id = $1", trackID).Scan(&pin); err != nil {
		t.Fatal(err)
	}
	return pin
}

func setCatalogMBID(t *testing.T, s store, trackID string, pin *string) {
	t.Helper()
	if _, err := s.pool.Exec(context.Background(), "UPDATE tracks SET catalog_mbid = $2 WHERE id = $1", trackID, pin); err != nil {
		t.Fatal(err)
	}
}

func seedBoundUnpinned(t *testing.T, s store, userID, recordingID string) libraryTrack {
	t.Helper()
	track := seedReady(t, s, userID)
	if _, err := s.save(t.Context(), uuid.New(), track, boundToRecording(track, recordingID)); err != nil {
		t.Fatal(err)
	}
	setCatalogMBID(t, s, track.ID, nil)
	return track
}

func runCatalogPinForTest(t *testing.T, s store, opts options) string {
	t.Helper()
	var out bytes.Buffer
	if err := runCatalogPin(t.Context(), s, opts, &out); err != nil {
		t.Fatalf("pin: %v\n%s", err, out.String())
	}
	return out.String()
}

func expectReport(t *testing.T, report string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(report, want) {
			t.Errorf("report missing %q:\n%s", want, report)
		}
	}
}

func TestRunPinCatalog_DryRunCountsAndSamplesButWritesNothing(t *testing.T) {
	s := testStore(t)
	user, recording := uuid.NewString(), uuid.NewString()
	track := seedBoundUnpinned(t, s, user, recording)

	report := runCatalogPinForTest(t, s, options{pinCatalog: true, userID: user})

	expectReport(t, report, "dry run, nothing written", "to pin     1", "conflict   0", "pin      Kool & The Gang - Celebration ("+track.ID+"): "+recording)
	if pin := catalogMBIDOf(t, s, track.ID); pin != nil {
		t.Errorf("a dry run pinned %s", *pin)
	}
}

func TestRunPinCatalog_ApplyPinsBoundTracksAndLeavesConflictsAndUnboundAlone(t *testing.T) {
	s := testStore(t)
	user, recording, other := uuid.NewString(), uuid.NewString(), uuid.NewString()
	unpinned := seedBoundUnpinned(t, s, user, recording)
	conflicting := seedBoundUnpinned(t, s, user, recording)
	setCatalogMBID(t, s, conflicting.ID, &other)
	unbound := seedReady(t, s, user)
	if _, err := s.save(t.Context(), uuid.New(), unbound, unboundIdentity(unbound)); err != nil {
		t.Fatal(err)
	}
	versionBefore := trackVersion(t, s, unpinned.ID)

	report := runCatalogPinForTest(t, s, options{pinCatalog: true, apply: true, userID: user})

	expectReport(t, report, "applied", "to pin     1", "conflict   1", "written    1", "has "+other+", bound to "+recording)
	if pin := catalogMBIDOf(t, s, unpinned.ID); pin == nil || *pin != recording {
		t.Errorf("unpinned track catalog_mbid = %v, want %s", pin, recording)
	}
	if pin := catalogMBIDOf(t, s, conflicting.ID); pin == nil || *pin != other {
		t.Errorf("conflicting track catalog_mbid = %v, want untouched %s", pin, other)
	}
	if pin := catalogMBIDOf(t, s, unbound.ID); pin != nil {
		t.Errorf("an unbound track was pinned to %s", *pin)
	}
	if got := trackVersion(t, s, unpinned.ID); got != versionBefore {
		t.Errorf("track version = %d, want unchanged %d", got, versionBefore)
	}
}

func TestRunPinCatalog_ApplyIsIdempotent(t *testing.T) {
	s := testStore(t)
	user := uuid.NewString()
	seedBoundUnpinned(t, s, user, uuid.NewString())
	runCatalogPinForTest(t, s, options{pinCatalog: true, apply: true, userID: user})

	report := runCatalogPinForTest(t, s, options{pinCatalog: true, apply: true, userID: user})

	expectReport(t, report, "to pin     0", "written    0")
}

func TestStore_PinCatalogSkipsATrackThatChangedSinceItWasRead(t *testing.T) {
	s := testStore(t)
	track := seedBoundUnpinned(t, s, uuid.NewString(), uuid.NewString())
	candidates, err := s.loadCatalogPinCandidates(t.Context(), track.UserID)
	if err != nil || len(candidates) != 1 {
		t.Fatalf("candidates = %+v, %v", candidates, err)
	}
	if _, err := s.pool.Exec(t.Context(), "UPDATE tracks SET version = version + 1 WHERE id = $1", track.ID); err != nil {
		t.Fatal(err)
	}
	verdict, err := s.writeCatalogPin(t.Context(), uuid.New(), candidates[0])
	if err != nil || verdict != saveSkipped {
		t.Fatalf("pin: %v, %v", verdict, err)
	}
	if pin := catalogMBIDOf(t, s, track.ID); pin != nil {
		t.Errorf("a stale read was pinned to %s", *pin)
	}
}

func TestStore_UndoRunRestoresNullOnlyWherePinIsStillTheOneWritten(t *testing.T) {
	s := testStore(t)
	user, recording, elsewhere := uuid.NewString(), uuid.NewString(), uuid.NewString()
	untouched := seedBoundUnpinned(t, s, user, recording)
	repinned := seedBoundUnpinned(t, s, user, recording)
	runCatalogPinForTest(t, s, options{pinCatalog: true, apply: true, userID: user})
	setCatalogMBID(t, s, repinned.ID, &elsewhere)
	var runID uuid.UUID
	if err := s.pool.QueryRow(t.Context(), "SELECT run_id FROM track_metadata_changes WHERE track_id = $1 AND field = 'catalog_mbid' ORDER BY applied_at DESC LIMIT 1", untouched.ID).Scan(&runID); err != nil {
		t.Fatal(err)
	}

	tally, err := s.undoRun(t.Context(), runID)

	if err != nil || tally.Unpinned != 1 || tally.PinsKept != 1 {
		t.Fatalf("undo: %+v, %v", tally, err)
	}
	if pin := catalogMBIDOf(t, s, untouched.ID); pin != nil {
		t.Errorf("the untouched pin survived the undo: %s", *pin)
	}
	if pin := catalogMBIDOf(t, s, repinned.ID); pin == nil || *pin != elsewhere {
		t.Errorf("a pin changed since the run was reset: %v", pin)
	}
}

func TestRunCatalogPin_LimitCountsOnlyTracksToPinNotConflicts(t *testing.T) {
	s := testStore(t)
	user, other := uuid.NewString(), uuid.NewString()
	conflicting := seedBoundUnpinned(t, s, user, uuid.NewString())
	setCatalogMBID(t, s, conflicting.ID, &other)
	seedBoundUnpinned(t, s, user, uuid.NewString())
	seedBoundUnpinned(t, s, user, uuid.NewString())

	report := runCatalogPinForTest(t, s, options{pinCatalog: true, userID: user, limit: 1})

	expectReport(t, report, "to pin     1", "conflict   1")
}
