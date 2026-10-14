package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/google/uuid"
)

const samplesPerStatus = 5

type identitySink interface {
	save(ctx context.Context, runID uuid.UUID, t libraryTrack, found identity) (saveVerdict, error)
}

type decided struct {
	Track libraryTrack
	Found identity
}

type tally struct {
	Decisions []decided
	Skipped   int
}

func gather(ctx context.Context, lookups []isrcLookup, t libraryTrack) ([]sourceOutcome, error) {
	if t.ISRC == "" {
		return nil, nil
	}
	outcomes := make([]sourceOutcome, 0, len(lookups))
	for _, lookup := range lookups {
		found, err := lookup.ByISRC(ctx, t.ISRC)
		if errors.Is(err, errCatalogBlocked) || ctx.Err() != nil {
			return nil, errors.Join(err, ctx.Err())
		}
		outcomes = append(outcomes, outcomeOf(t, lookup.Name(), found, err))
	}
	return outcomes, nil
}

func bindTracks(ctx context.Context, tracks []libraryTrack, lookups []isrcLookup, sink identitySink, runID uuid.UUID) (tally, error) {
	var done tally
	for _, t := range tracks {
		outcomes, err := gather(ctx, lookups, t)
		if err != nil {
			return done, fmt.Errorf("aborted after %d of %d tracks: %w", len(done.Decisions), len(tracks), err)
		}
		found := decide(t, outcomes)
		done.Decisions = append(done.Decisions, decided{Track: t, Found: found})
		if sink == nil {
			continue
		}
		verdict, err := sink.save(ctx, runID, t, found)
		if err != nil {
			return done, fmt.Errorf("aborted after %d of %d tracks: %w", len(done.Decisions), len(tracks), err)
		}
		if verdict == saveSkipped {
			done.Skipped++
		}
	}
	return done, nil
}

func writeReport(out io.Writer, done tally, apply bool, runID uuid.UUID) {
	mode := "dry run, nothing written"
	if apply {
		mode = "applied"
	}
	_, _ = fmt.Fprintf(out, "run %s (%s): %d tracks\n", runID, mode, len(done.Decisions))
	for _, st := range []status{statusBound, statusUnofficial, statusUnbound} {
		_, _ = fmt.Fprintf(out, "  %-10s %d\n", st, countStatus(done.Decisions, st))
	}
	if done.Skipped > 0 {
		_, _ = fmt.Fprintf(out, "  skipped (track changed or already identified): %d\n", done.Skipped)
	}
	_, _ = fmt.Fprintln(out, "reasons:")
	for _, line := range reasonLines(done.Decisions) {
		_, _ = fmt.Fprintln(out, line)
	}
	_, _ = fmt.Fprintln(out, "sample:")
	for _, line := range sampleLines(done.Decisions) {
		_, _ = fmt.Fprintln(out, line)
	}
}

func countStatus(decisions []decided, want status) int {
	return len(slices.DeleteFunc(slices.Clone(decisions), func(d decided) bool { return d.Found.Status != want }))
}

func reasonLines(decisions []decided) []string {
	counts := map[string]int{}
	for _, d := range decisions {
		counts[string(d.Found.Status)+"\t"+d.Found.Reason]++
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, fmt.Sprintf("  %5d  %s", counts[key], strings.Replace(key, "\t", "  ", 1)))
	}
	return lines
}

func sampleLines(decisions []decided) []string {
	shown := map[status]int{}
	var lines []string
	for _, d := range decisions {
		if shown[d.Found.Status] >= samplesPerStatus {
			continue
		}
		shown[d.Found.Status]++
		lines = append(lines, fmt.Sprintf("  %-10s %s - %s (%s): %s", d.Found.Status, d.Track.Artist, d.Track.Title, d.Track.ID, d.Found.Reason))
	}
	return lines
}
