package main

import (
	"altune/go-api/internal/acquisition"
	"bytes"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestWriteTally_DryRunPrintsACountPerGradeAndKind(t *testing.T) {
	counts := newTally()
	counts.grades[acquisition.GradeA] = 7
	counts.grades[acquisition.GradeF] = 2
	counts.kinds[acquisition.ArtTrack] = 5
	counts.kinds[acquisition.FanUpload] = 4
	counts.unresolved = 1
	var out bytes.Buffer

	writeTally(&out, counts, false, uuid.New())

	for _, want := range []string{"DRY RUN, nothing written", "grade A: 7", "grade B: 0", "grade F: 2", "art_track:      5", "fan_upload:     4", "unresolved:     1"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in:\n%s", want, out.String())
		}
	}
}

func TestWriteTally_ApplyNamesTheRunToUndo(t *testing.T) {
	runID := uuid.New()
	var out bytes.Buffer

	writeTally(&out, newTally(), true, runID)

	if !strings.Contains(out.String(), "run_id "+runID.String()) {
		t.Errorf("out = %s", out.String())
	}
}
