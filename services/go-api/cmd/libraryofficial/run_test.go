package main

import (
	"altune/go-api/internal/acquisition/adapters/ytmsearch"
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

type recordingSink struct {
	pointed map[string]string
	verdict writeVerdict
	err     error
}

func (r *recordingSink) point(_ context.Context, _ uuid.UUID, t target, videoID string) (writeVerdict, error) {
	if r.err != nil {
		return writeSkipped, r.err
	}
	if r.pointed == nil {
		r.pointed = map[string]string{}
	}
	r.pointed[t.TrackID] = videoID
	return r.verdict, nil
}

func trackNamed(id, title string) target {
	track := theTrack()
	track.TrackID, track.Title, track.ISRC = id, title, "ISRC-"+id
	return track
}

func songTitled(title, videoID string) ytmsearch.Song {
	song := artTrack()
	song.Title, song.VideoID = title, videoID
	return song
}

func libraryOfThree() ([]target, *scriptedSearch) {
	targets := []target{trackNamed("t1", "Alpha"), trackNamed("t2", "Beta"), trackNamed("t3", "Gamma")}
	return targets, &scriptedSearch{answers: map[string][]ytmsearch.Song{
		"ISRC-t1": {songTitled("Alpha", "AAAAAAAAAA1")},
		"ISRC-t2": {songTitled("Beta", "BBBBBBBBBB1"), songTitled("Beta", "BBBBBBBBBB2")},
	}}
}

func TestRunOfficial_DryRunCountsEachOutcome(t *testing.T) {
	targets, search := libraryOfThree()
	done, err := runOfficial(t.Context(), search, targets, nil, 2, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if len(done.ByKind[outcomeFound]) != 1 || len(done.ByKind[outcomeAmbiguous]) != 1 || len(done.ByKind[outcomeNone]) != 1 {
		t.Fatalf("got %+v", done.ByKind)
	}
}

func TestRunOfficial_ApplyPointsOnlyTheFound(t *testing.T) {
	targets, search := libraryOfThree()
	sink := &recordingSink{verdict: writeDone}
	done, err := runOfficial(t.Context(), search, targets, sink, 2, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if len(sink.pointed) != 1 || sink.pointed["t1"] != "AAAAAAAAAA1" || done.Written != 1 {
		t.Fatalf("pointed %v, written %d", sink.pointed, done.Written)
	}
}

func TestRunOfficial_ASkippedWriteIsCountedSkipped(t *testing.T) {
	targets, search := libraryOfThree()
	done, err := runOfficial(t.Context(), search, targets, &recordingSink{verdict: writeSkipped}, 1, uuid.New())
	if err != nil || done.Skipped != 1 || done.Written != 0 {
		t.Fatalf("got %+v, %v", done, err)
	}
}

func TestRunOfficial_AWriteFailureAborts(t *testing.T) {
	targets, search := libraryOfThree()
	boom := errors.New("db down")
	_, err := runOfficial(t.Context(), search, targets, &recordingSink{err: boom}, 2, uuid.New())
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}

func TestRunOfficial_StopsWhenSearchesKeepFailing(t *testing.T) {
	targets := make([]target, maxSearchFailures+10)
	for i := range targets {
		targets[i] = trackNamed(uuid.NewString(), "Alpha")
	}
	search := &scriptedSearch{err: errors.New("429")}
	done, err := runOfficial(t.Context(), search, targets, nil, 1, uuid.New())
	if !errors.Is(err, errTooManySearchFailures) || len(done.ByKind[outcomeErrored]) != maxSearchFailures {
		t.Fatalf("err %v, errored %d", err, len(done.ByKind[outcomeErrored]))
	}
}

func TestWriteReport_NamesFoundAmbiguousNoneAndWhyNone(t *testing.T) {
	targets, search := libraryOfThree()
	done, _ := runOfficial(t.Context(), search, targets, nil, 1, uuid.New())
	var out bytes.Buffer
	writeReport(&out, done, false, uuid.New())
	for _, want := range []string{"dry run, nothing written", "found      1", "ambiguous  1", "none       1", "no-song-results", "found      Cutting Crew - Alpha (t1): AAAAAAAAAA1"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("report missing %q:\n%s", want, out.String())
		}
	}
}
