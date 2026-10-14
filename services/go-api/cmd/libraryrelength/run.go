package main

import (
	"context"
	"fmt"
	"io"

	"github.com/google/uuid"
)

func describe(c change, v verdict, write bool) string {
	label := string(v)
	if v == verdictApplied && !write {
		label = "would apply"
	}
	return fmt.Sprintf("%-24s %-9s %s - %s (%s)%s", label, c.Action, c.Artist, c.Title, c.TrackID, detail(c))
}

func detail(c change) string {
	if c.Action == actionFixISRC {
		return fmt.Sprintf(": isrc %s -> %s, catalog %ds", c.OldISRC, c.NewISRC, int(c.CatalogSeconds))
	}
	return fmt.Sprintf(": file %ds vs catalog %ds", int(c.FileSeconds), int(c.CatalogSeconds))
}

func runPlan(ctx context.Context, s store, changes []change, limit int, write bool, runID uuid.UUID, out io.Writer) error {
	counts := map[verdict]int{}
	enqueued := 0
	for _, c := range changes {
		v := applyWithinCap(ctx, s, runID, c, write, enqueued >= limit, out)
		counts[v]++
		if c.Action == actionReacquire && v == verdictApplied {
			enqueued++
		}
	}
	writeTally(out, counts, limit, write, runID)
	if bad := counts[verdictFailed] + counts[verdictEnqueueFailed]; bad > 0 {
		return fmt.Errorf("%d changes failed", bad)
	}
	return nil
}

func applyWithinCap(ctx context.Context, s store, runID uuid.UUID, c change, write, capReached bool, out io.Writer) verdict {
	if c.Action == actionReacquire && capReached {
		_, _ = fmt.Fprintln(out, describe(c, verdictDeferred, write))
		return verdictDeferred
	}
	v, err := s.apply(ctx, runID, c, write)
	_, _ = fmt.Fprintln(out, describe(c, v, write))
	if err != nil {
		_, _ = fmt.Fprintf(out, "  error: %v\n", err)
	}
	return v
}

func writeTally(out io.Writer, counts map[verdict]int, limit int, write bool, runID uuid.UUID) {
	mode, applied := "DRY RUN, nothing written", "would apply:"
	if write {
		mode, applied = "run_id "+runID.String(), "applied:"
	}
	_, _ = fmt.Fprintf(out, "%s (re-acquire cap %d per run)\n  %-17s%d\n  deferred-by-cap: %d\n  stale:           %d\n  skipped:         %d\n  failed:          %d\n",
		mode, limit, applied, counts[verdictApplied], counts[verdictDeferred], counts[verdictStale], skipped(counts),
		counts[verdictFailed]+counts[verdictEnqueueFailed])
}

func skipped(counts map[verdict]int) int {
	return counts[verdictISRCChanged] + counts[verdictNotReady] + counts[verdictInFlight] +
		counts[verdictAlreadyEnqueued] + counts[verdictFileChanged]
}

func runUndo(ctx context.Context, s store, runID uuid.UUID, out io.Writer) error {
	results, err := s.undoRun(ctx, runID)
	if err != nil {
		return err
	}
	counts := map[undoVerdict]int{}
	for _, r := range results {
		counts[r.Verdict]++
		_, _ = fmt.Fprintf(out, "%-24s %-9s track %s\n", r.Verdict, r.Fix.Action, r.Fix.TrackID)
		if r.Err != nil {
			_, _ = fmt.Fprintf(out, "  error: %v\n", r.Err)
		}
	}
	_, _ = fmt.Fprintf(out, "undo of run_id %s\n  restored: %d\n  skipped:  %d\n  failed:   %d\n",
		runID, counts[undoRestored], len(results)-counts[undoRestored]-counts[undoFailed], counts[undoFailed])
	if counts[undoFailed] > 0 {
		return fmt.Errorf("%d undos failed", counts[undoFailed])
	}
	return nil
}
