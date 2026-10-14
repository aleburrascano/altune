package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

type scriptedLookup struct {
	name  source
	found []candidate
	err   error
	calls *int
}

func (s scriptedLookup) Name() source { return s.name }

func (s scriptedLookup) ByISRC(context.Context, string) ([]candidate, error) {
	*s.calls++
	return s.found, s.err
}

type recordingSink struct{ saved []identity }

func (r *recordingSink) save(_ context.Context, _ uuid.UUID, _ libraryTrack, found identity) (saveVerdict, error) {
	r.saved = append(r.saved, found)
	return saveWritten, nil
}

func TestBindTracks_CountsEveryTrackAndWritesOnlyThroughTheSink(t *testing.T) {
	calls := 0
	lookups := []isrcLookup{scriptedLookup{name: sourceDeezer, found: []candidate{catalogCelebration("77")}, calls: &calls}}
	unreleased := with2(celebration(), func(tr *libraryTrack) { tr.ID, tr.ISRC, tr.Album = "track-2", "", "UNRELEASED" })
	noisrc := with2(celebration(), func(tr *libraryTrack) { tr.ID, tr.ISRC = "track-3", "" })
	tracks := []libraryTrack{celebration(), unreleased, noisrc}

	dry, err := bindTracks(t.Context(), tracks, lookups, nil, uuid.New())
	if err != nil || len(dry.Decisions) != 3 {
		t.Fatalf("dry run: %+v, %v", dry, err)
	}
	if calls != 1 {
		t.Errorf("lookups = %d, want 1 (tracks without an isrc are not looked up)", calls)
	}

	sink := &recordingSink{}
	if _, err := bindTracks(t.Context(), tracks, lookups, sink, uuid.New()); err != nil || len(sink.saved) != 3 {
		t.Fatalf("apply: %d saved, %v", len(sink.saved), err)
	}
}

func TestBindTracks_StopsWhenACatalogBlocks(t *testing.T) {
	calls := 0
	lookups := []isrcLookup{scriptedLookup{name: sourceDeezer, err: blocked("HTTP 403"), calls: &calls}}
	sink := &recordingSink{}
	done, err := bindTracks(t.Context(), []libraryTrack{celebration(), celebration()}, lookups, sink, uuid.New())
	if !errors.Is(err, errCatalogBlocked) {
		t.Fatalf("err = %v", err)
	}
	if calls != 1 || len(sink.saved) != 0 || len(done.Decisions) != 0 {
		t.Errorf("calls %d, saved %d, decided %d", calls, len(sink.saved), len(done.Decisions))
	}
}

func TestWriteReport_PrintsCountsPerStatusAndReasonWithASample(t *testing.T) {
	calls := 0
	lookups := []isrcLookup{scriptedLookup{name: sourceDeezer, found: []candidate{catalogCelebration("77")}, calls: &calls}}
	noisrc := with2(celebration(), func(tr *libraryTrack) { tr.ID, tr.ISRC = "track-3", "" })
	runID := uuid.New()
	done, err := bindTracks(t.Context(), []libraryTrack{celebration(), noisrc}, lookups, nil, runID)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	writeReport(&out, done, false, runID)
	for _, want := range []string{"dry run, nothing written", "bound      1", "unbound    1", "unofficial 0", "1  unbound  no-isrc", "1  bound  isrc confirmed by deezer", "Kool & The Gang - Celebration (track-3)"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("report missing %q:\n%s", want, out.String())
		}
	}
}
