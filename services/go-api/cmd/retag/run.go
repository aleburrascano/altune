package main

import (
	"context"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
)

type tally map[verdict]int

func markSharedRefs(tracks []trackRow) (work, shared []trackRow) {
	holders := map[string]int{}
	for _, track := range tracks {
		holders[track.Ref]++
	}
	for _, track := range tracks {
		if holders[track.Ref] > 1 {
			shared = append(shared, track)
			continue
		}
		work = append(work, track)
	}
	return work, shared
}

func newClaim(limit int) func() bool {
	if limit <= 0 {
		return func() bool { return true }
	}
	var claimed atomic.Int64
	return func() bool { return claimed.Add(1) <= int64(limit) }
}

func processAll(ctx context.Context, r retagger, tracks []trackRow, apply bool, concurrency int) []outcome {
	outcomes := make([]outcome, len(tracks))
	slots := make(chan struct{}, max(concurrency, 1))
	var group sync.WaitGroup
	for i, track := range tracks {
		slots <- struct{}{}
		group.Add(1)
		go func() {
			defer group.Done()
			defer func() { <-slots }()
			outcomes[i] = r.process(ctx, track, apply)
		}()
	}
	group.Wait()
	return outcomes
}

func describe(o outcome) string {
	line := fmt.Sprintf("%-12s track %s %s", o.Verdict, o.Track.ID, o.Track.Ref)
	if o.Err != nil {
		line += fmt.Sprintf("\n  error: %v", o.Err)
	}
	for _, d := range o.Diffs {
		line += fmt.Sprintf("\n  %s: %q -> %q", d.Field, d.Got, d.Want)
	}
	if o.Leftover != "" {
		line += "\n  staged object not deleted: " + o.Leftover
	}
	return line
}

type trackSource interface {
	readyTracks(ctx context.Context, sel selection) ([]trackRow, error)
}

func runRetag(ctx context.Context, tracks trackSource, r retagger, sel selection, apply bool, concurrency int, out io.Writer) error {
	rows, err := tracks.readyTracks(ctx, sel)
	if err != nil {
		return err
	}
	work, shared := markSharedRefs(rows)
	outcomes := processAll(ctx, r, work, apply, concurrency)
	for _, track := range shared {
		outcomes = append(outcomes, outcome{Track: track, Verdict: verdictSharedRef})
	}
	return report(out, outcomes, apply)
}

func report(out io.Writer, outcomes []outcome, apply bool) error {
	counts := tally{}
	for _, o := range outcomes {
		counts[o.Verdict]++
		if o.Verdict != verdictMatched {
			_, _ = fmt.Fprintln(out, describe(o))
		}
	}
	writeTally(out, counts, apply, len(outcomes))
	if counts[verdictFailed] > 0 {
		return fmt.Errorf("%d tracks failed", counts[verdictFailed])
	}
	return nil
}

func writeTally(out io.Writer, counts tally, apply bool, total int) {
	mode, changeLabel, changed := "DRY RUN, nothing written", "would retag:", counts[verdictWouldRetag]
	if apply {
		mode, changeLabel, changed = "APPLIED", "retagged:", counts[verdictRetagged]
	}
	_, _ = fmt.Fprintf(out, "%s\n  examined:      %d\n  already match: %d\n  %-14s %d\n  moved:         %d\n  shared-ref:    %d\n  over limit:    %d\n  failed:        %d\n",
		mode, total, counts[verdictMatched], changeLabel, changed,
		counts[verdictMoved], counts[verdictSharedRef], counts[verdictLimitReached], counts[verdictFailed])
}
