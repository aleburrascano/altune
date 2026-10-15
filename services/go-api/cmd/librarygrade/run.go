package main

import (
	"altune/go-api/internal/acquisition/service/grade"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/google/uuid"
)

var errNothingToUndo = errors.New("nothing to undo")

var gradeOrder = []grade.Grade{grade.F, grade.D, grade.C, grade.B, grade.A}

var kindOrder = []grade.SourceKind{
	grade.ArtTrack, grade.OfficialVideo, grade.LabelUpload, grade.FanUpload, grade.UserUpload,
}

type tally struct {
	grades     map[grade.Grade]int
	kinds      map[grade.SourceKind]int
	unresolved int
	stale      int
	failed     int
}

func newTally() *tally {
	return &tally{grades: map[grade.Grade]int{}, kinds: map[grade.SourceKind]int{}}
}

func runBackfill(ctx context.Context, s store, lookup authorLookup, opts options, runID uuid.UUID, out io.Writer) error {
	rows, err := s.ungradedTracks(ctx, opts.userID, opts.limit)
	if err != nil {
		return err
	}
	counts := newTally()
	for _, row := range rows {
		backfillTrack(ctx, s, lookup, row, opts.apply, runID, counts, out)
	}
	writeTally(out, counts, opts.apply, runID)
	if counts.failed > 0 {
		return fmt.Errorf("%d tracks failed", counts.failed)
	}
	return nil
}

func backfillTrack(ctx context.Context, s store, lookup authorLookup, row trackRow, write bool, runID uuid.UUID, counts *tally, out io.Writer) {
	p, err := planTrack(ctx, lookup, row, runID.String())
	if err != nil {
		counts.unresolved++
		_, _ = fmt.Fprintf(out, "unresolved         track %s: %v\n", row.ID, err)
		return
	}
	if write && !persist(ctx, s, row, p, counts, out) {
		return
	}
	counts.grades[p.Grade]++
	counts.kinds[p.SourceKind]++
	_, _ = fmt.Fprintf(out, "%-18s track %s %s grade %s\n", verbFor(write), row.ID, p.SourceKind, p.Grade)
}

func persist(ctx context.Context, s store, row trackRow, p patch, counts *tally, out io.Writer) bool {
	applied, err := s.applyPatch(ctx, row, p)
	switch {
	case err != nil:
		counts.failed++
		_, _ = fmt.Fprintf(out, "failed             track %s: %v\n", row.ID, err)
	case !applied:
		counts.stale++
		_, _ = fmt.Fprintf(out, "stale              track %s\n", row.ID)
	}
	return err == nil && applied
}

func verbFor(write bool) string {
	if write {
		return "applied:"
	}
	return "would apply:"
}

func writeTally(out io.Writer, counts *tally, write bool, runID uuid.UUID) {
	mode := "DRY RUN, nothing written"
	if write {
		mode = "run_id " + runID.String()
	}
	_, _ = fmt.Fprintln(out, mode)
	for _, each := range gradeOrder {
		_, _ = fmt.Fprintf(out, "  grade %s: %d\n", each, counts.grades[each])
	}
	for _, kind := range kindOrder {
		_, _ = fmt.Fprintf(out, "  %-15s %d\n", kind+":", counts.kinds[kind])
	}
	_, _ = fmt.Fprintf(out, "  unresolved:     %d\n  stale:          %d\n  failed:         %d\n", counts.unresolved, counts.stale, counts.failed)
}

func runUndo(ctx context.Context, s store, runID uuid.UUID, out io.Writer) error {
	restored, err := s.undoRun(ctx, runID)
	if err != nil {
		return err
	}
	if restored == 0 {
		return fmt.Errorf("%w for run_id %s", errNothingToUndo, runID)
	}
	_, _ = fmt.Fprintf(out, "undo of run_id %s\n  restored: %d\n", runID, restored)
	return nil
}
