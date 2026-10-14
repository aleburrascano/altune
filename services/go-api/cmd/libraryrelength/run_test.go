package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func seedReacquires(t *testing.T, s store, count int) []change {
	t.Helper()
	changes := make([]change, 0, count)
	for range count {
		row := seedReady(t, s, "USALBUM", 226)
		changes = append(changes, changeFor(row, actionReacquire, "", 226, 379))
	}
	return changes
}

func queuedCount(t *testing.T, s store, changes []change) int {
	t.Helper()
	queued := 0
	for _, c := range changes {
		if stateOf(t, s, seeded{trackID: c.TrackID, userID: c.UserID}).jobQueued {
			queued++
		}
	}
	return queued
}

func TestRunPlan_EnqueuesAtMostTheCapPerRunAndDefersTheRest(t *testing.T) {
	s := testStore(t)
	changes := seedReacquires(t, s, 5)
	var out bytes.Buffer
	if err := runPlan(context.Background(), s, changes, 2, true, uuid.New(), &out); err != nil {
		t.Fatal(err)
	}
	if got := queuedCount(t, s, changes); got != 2 {
		t.Fatalf("queued %d, want 2\n%s", got, out.String())
	}
	if strings.Count(out.String(), "deferred-by-cap") != 4 || !strings.Contains(out.String(), "applied:         2") {
		t.Fatalf("output:\n%s", out.String())
	}
	if !stateOf(t, s, seeded{trackID: changes[0].TrackID}).jobQueued || stateOf(t, s, seeded{trackID: changes[2].TrackID}).jobQueued {
		t.Fatal("the cap must keep report order")
	}
}

func TestRunPlan_ASkippedTrackDoesNotSpendTheCap(t *testing.T) {
	s := testStore(t)
	changes := seedReacquires(t, s, 3)
	changes[0].OldISRC = "USMOVED"
	var out bytes.Buffer
	if err := runPlan(context.Background(), s, changes, 2, true, uuid.New(), &out); err != nil {
		t.Fatal(err)
	}
	if got := queuedCount(t, s, changes); got != 2 || stateOf(t, s, seeded{trackID: changes[0].TrackID}).jobQueued {
		t.Fatalf("queued %d\n%s", got, out.String())
	}
}

func TestRunPlan_ISRCFixesAreNotCapped(t *testing.T) {
	s := testStore(t)
	var changes []change
	for range 3 {
		changes = append(changes, changeFor(seedReady(t, s, "USALBUM", 226), actionFixISRC, "USRADIO", 226, 225))
	}
	var out bytes.Buffer
	if err := runPlan(context.Background(), s, changes, 1, true, uuid.New(), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "applied:         3") {
		t.Fatalf("output:\n%s", out.String())
	}
}

func TestRunPlan_DryRunWritesNothingAndSaysSo(t *testing.T) {
	s := testStore(t)
	changes := seedReacquires(t, s, 3)
	var out bytes.Buffer
	if err := runPlan(context.Background(), s, changes, 2, false, uuid.New(), &out); err != nil {
		t.Fatal(err)
	}
	for _, c := range changes {
		if got := stateOf(t, s, seeded{trackID: c.TrackID}); got.jobQueued || got.loggedFixes != 0 || got.catalog != nil || got.version != 4 {
			t.Fatalf("dry run wrote %+v", got)
		}
	}
	if !strings.Contains(out.String(), "DRY RUN, nothing written") || strings.Count(out.String(), "would apply") != 3 {
		t.Fatalf("output:\n%s", out.String())
	}
}

func TestLoadStatus_ReportsEachEnqueuedTrackOutcome(t *testing.T) {
	s := testStore(t)
	changes := seedReacquires(t, s, 3)
	if err := runPlan(context.Background(), s, changes, 3, true, uuid.New(), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	settle := `UPDATE tracks SET acquisition_available_at = NULL WHERE id = $1`
	replaceFits := `UPDATE tracks SET acquisition_available_at = NULL, audio_version = 'v2', duration_seconds = 380, audio_length_mismatch = false WHERE id = $1`
	replaceOff := `UPDATE tracks SET acquisition_available_at = NULL, audio_version = 'v2', duration_seconds = 150, audio_length_mismatch = true WHERE id = $1`
	for i, sql := range []string{settle, replaceFits, replaceOff} {
		if _, err := s.pool.Exec(context.Background(), sql, changes[i].TrackID); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := loadStatus(context.Background(), s.pool)
	if err != nil {
		t.Fatal(err)
	}
	byTrack := map[string]statusRow{}
	for _, r := range rows {
		byTrack[r.TrackID] = r
	}
	kept, fits, off := byTrack[changes[0].TrackID], byTrack[changes[1].TrackID], byTrack[changes[2].TrackID]
	if kept.outcome() != outcomeNotReplaced || fits.outcome() != outcomeFits || off.outcome() != outcomeStillOff {
		t.Fatalf("got %v %v %v", kept.outcome(), fits.outcome(), off.outcome())
	}
	if kept.listenFileSeconds() != 226 || off.listenFileSeconds() != 150 || off.CatalogSeconds != 379 {
		t.Fatalf("listen lengths %v %v %v", kept.listenFileSeconds(), off.listenFileSeconds(), off.CatalogSeconds)
	}
}

func TestRunPlan_StopsAfterConsecutiveEnqueueFailuresAndLeavesTheRestUntouched(t *testing.T) {
	s := testStore(t)
	s.queue = failingQueue{}
	changes := seedReacquires(t, s, 6)
	var out bytes.Buffer
	err := runPlan(context.Background(), s, changes, 10, true, uuid.New(), &out)
	if err == nil || !strings.Contains(err.Error(), "3 changes not attempted") {
		t.Fatalf("err %v\n%s", err, out.String())
	}
	if strings.Count(out.String(), "enqueue-failed") != 3 || !strings.Contains(out.String(), "not attempted:   3") {
		t.Fatalf("output:\n%s", out.String())
	}
	for _, c := range changes[3:] {
		if got := stateOf(t, s, seeded{trackID: c.TrackID}); got.loggedFixes != 0 || got.catalog != nil {
			t.Fatalf("untouched track changed %+v", got)
		}
	}
}
