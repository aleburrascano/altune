package main

import (
	"context"
	"fmt"
	"io"

	"github.com/google/uuid"
)

func valueText(value *string) string {
	if value == nil {
		return "NULL"
	}
	return fmt.Sprintf("%q", *value)
}

func describe(d decision, write bool) string {
	label := string(d.Verdict)
	if d.Verdict == verdictApplied && !write {
		label = "would apply:"
	}
	return fmt.Sprintf("%-18s track %s %s: %s -> %s", label, d.Change.TrackID, d.Change.Field, valueText(d.Current), valueText(d.Change.NewValue))
}

type tally map[verdict]int

func runReconcile(ctx context.Context, s store, plans []trackPlan, write bool, runID uuid.UUID, out io.Writer) error {
	counts := tally{}
	for _, plan := range plans {
		decisions, err := s.reconcileTrack(ctx, runID, plan, write)
		if err != nil {
			_, _ = fmt.Fprintf(out, "failed             track %s: %v\n", plan.TrackID, err)
		}
		for _, d := range decisions {
			counts[d.Verdict]++
			if err == nil {
				_, _ = fmt.Fprintln(out, describe(d, write))
			}
		}
	}
	writeTally(out, counts, write, runID)
	if counts[verdictFailed] > 0 {
		return fmt.Errorf("%d changes failed", counts[verdictFailed])
	}
	return nil
}

func writeTally(out io.Writer, counts tally, write bool, runID uuid.UUID) {
	mode, applied := "DRY RUN, nothing written", "would apply:"
	if write {
		mode, applied = "run_id "+runID.String(), "applied:"
	}
	_, _ = fmt.Fprintf(out, "%s\n  %-19s%d\n  skipped-not-empty: %d\n  stale:             %d\n  failed:            %d\n",
		mode, applied, counts[verdictApplied], counts[verdictSkippedNotEmpty], counts[verdictStale], counts[verdictFailed])
}

func runUndo(ctx context.Context, s store, runID uuid.UUID, out io.Writer) error {
	results, err := s.undoRun(ctx, runID)
	if err != nil {
		return err
	}
	counts := map[undoVerdict]int{}
	for _, r := range results {
		counts[r.Verdict]++
		_, _ = fmt.Fprintf(out, "%-16s track %s %s\n", r.Verdict, r.Change.TrackID, r.Change.Field)
		if r.Err != nil {
			_, _ = fmt.Fprintf(out, "  error: %v\n", r.Err)
		}
	}
	_, _ = fmt.Fprintf(out, "undo of run_id %s\n  restored:        %d\n  skipped-changed: %d\n  failed:          %d\n",
		runID, counts[undoRestored], counts[undoSkippedChanged], counts[undoFailed])
	if counts[undoFailed] > 0 {
		return fmt.Errorf("%d undos failed", counts[undoFailed])
	}
	return nil
}
