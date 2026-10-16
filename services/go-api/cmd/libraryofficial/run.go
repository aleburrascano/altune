package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"sync"

	"github.com/google/uuid"
)

const (
	samplesPerKind    = 5
	maxSearchFailures = 3
)

const noSongResults rejection = "no-song-results"

var errTooManySearchFailures = errors.New("YouTube Music is refusing searches, stopping")

type pointer interface {
	point(ctx context.Context, runID uuid.UUID, t target, videoID string) (writeVerdict, error)
}

type tally struct {
	ByKind  map[outcomeKind][]outcome
	Written int
	Skipped int
}

func feed(ctx context.Context, targets []target, jobs chan<- target) {
	defer close(jobs)
	for _, t := range targets {
		select {
		case jobs <- t:
		case <-ctx.Done():
			return
		}
	}
}

func work(ctx context.Context, search songSearch, jobs <-chan target, results chan<- outcome) {
	for t := range jobs {
		select {
		case results <- find(ctx, search, t):
		case <-ctx.Done():
			return
		}
	}
}

func findAll(ctx context.Context, search songSearch, targets []target, concurrency int) <-chan outcome {
	jobs := make(chan target)
	results := make(chan outcome)
	var workers sync.WaitGroup
	for range concurrency {
		workers.Go(func() { work(ctx, search, jobs, results) })
	}
	go feed(ctx, targets, jobs)
	go func() {
		workers.Wait()
		close(results)
	}()
	return results
}

func (done *tally) record(ctx context.Context, sink pointer, runID uuid.UUID, result outcome) error {
	done.ByKind[result.Kind] = append(done.ByKind[result.Kind], result)
	if len(done.ByKind[outcomeErrored]) >= maxSearchFailures {
		return fmt.Errorf("%w: %d searches failed, the last: %w", errTooManySearchFailures, maxSearchFailures, result.Err)
	}
	if sink == nil || result.Kind != outcomeFound {
		return nil
	}
	verdict, err := sink.point(ctx, runID, result.Target, result.VideoID)
	if err != nil {
		return fmt.Errorf("aborted after %d writes: %w", done.Written, err)
	}
	if verdict == writeDone {
		done.Written++
		return nil
	}
	done.Skipped++
	return nil
}

func runOfficial(ctx context.Context, search songSearch, targets []target, sink pointer, concurrency int, runID uuid.UUID) (tally, error) {
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	done := tally{ByKind: map[outcomeKind][]outcome{}}
	for result := range findAll(ctx, search, targets, concurrency) {
		if err := done.record(ctx, sink, runID, result); err != nil {
			return done, err
		}
	}
	return done, ctx.Err()
}

var reportedKinds = []outcomeKind{outcomeFound, outcomeAmbiguous, outcomeNone, outcomeErrored}

func writeReport(out io.Writer, done tally, apply bool, runID uuid.UUID) {
	mode := "dry run, nothing written"
	if apply {
		mode = "applied"
	}
	_, _ = fmt.Fprintf(out, "run %s (%s): official YouTube Music art-track pointers\n", runID, mode)
	for _, kind := range reportedKinds {
		_, _ = fmt.Fprintf(out, "  %-10s %d\n", kind, len(done.ByKind[kind]))
	}
	if apply {
		_, _ = fmt.Fprintf(out, "  written    %d\n  skipped    %d (track changed or already pointed since it was read)\n", done.Written, done.Skipped)
	}
	_, _ = fmt.Fprintln(out, "none, by what the candidates failed on:")
	for _, line := range rejectionLines(done.ByKind[outcomeNone]) {
		_, _ = fmt.Fprintln(out, line)
	}
	writeSamples(out, done)
}

func writeSamples(out io.Writer, done tally) {
	_, _ = fmt.Fprintln(out, "sample:")
	for _, kind := range reportedKinds {
		for _, result := range done.ByKind[kind][:min(len(done.ByKind[kind]), samplesPerKind)] {
			_, _ = fmt.Fprintf(out, "  %-10s %s - %s (%s): %s\n", kind, result.Target.Artist, result.Target.Title, result.Target.TrackID, detailOf(result))
		}
	}
}

func distinctReasons(result outcome) []rejection {
	return slices.Compact(slices.Sorted(slices.Values(result.Reasons)))
}

func reasonCounts(none []outcome) map[rejection]int {
	counts := map[rejection]int{}
	for _, result := range none {
		if len(result.Reasons) == 0 {
			counts[noSongResults]++
		}
		for _, reason := range distinctReasons(result) {
			counts[reason]++
		}
	}
	return counts
}

func rejectionLines(none []outcome) []string {
	counts := reasonCounts(none)
	var lines []string
	for _, reason := range slices.Sorted(maps.Keys(counts)) {
		lines = append(lines, fmt.Sprintf("  %5d  %s", counts[reason], reason))
	}
	return lines
}

func detailOf(result outcome) string {
	switch result.Kind {
	case outcomeFound:
		return result.VideoID
	case outcomeAmbiguous:
		return fmt.Sprint(result.VideoIDs)
	case outcomeErrored:
		return result.Err.Error()
	}
	return fmt.Sprint(distinctReasons(result))
}
