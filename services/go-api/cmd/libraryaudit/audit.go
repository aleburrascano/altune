package main

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
)

type catalog interface {
	TrackByISRC(ctx context.Context, isrc string) (catalogTrack, error)
	Search(ctx context.Context, artist, title string) ([]catalogTrack, error)
	TrackByID(ctx context.Context, id int64) (catalogTrack, error)
}

type coverJudge interface {
	SameImage(ctx context.Context, libraryURL, catalogURL string) (bool, error)
}

type trackResult struct {
	Checked  bool
	Findings []finding
	Blocked  error
	Matched  *catalogTrack
}

func lookupFailure(ctx context.Context, lib libraryTrack, err error) trackResult {
	switch {
	case errors.Is(err, errCatalogBlocked):
		return trackResult{Blocked: err}
	case ctx.Err() != nil:
		return trackResult{}
	default:
		return trackResult{Findings: []finding{about(lib, categoryLookupFailed, err.Error())}}
	}
}

func auditTrack(ctx context.Context, cat catalog, covers coverJudge, lib libraryTrack) trackResult {
	if strings.TrimSpace(lib.ISRC) == "" {
		resolved, err := resolveMissingISRC(ctx, cat, lib)
		if err != nil {
			return lookupFailure(ctx, lib, err)
		}
		return trackResult{Findings: append([]finding{about(lib, categoryNoISRC, resolved.reason)}, resolved.findings...)}
	}
	found, err := catalogRecord(ctx, cat, lib)
	switch {
	case errors.Is(err, errCatalogUnknown):
		return confirmUnknownISRC(ctx, cat, lib)
	case err != nil:
		return lookupFailure(ctx, lib, err)
	}
	findings := metadataFindings(lib, found)
	if hasCategory(findings, categoryWrongSong) {
		confirmed, kept, err := musicBrainzKeepsISRC(ctx, cat, lib, found)
		switch {
		case err != nil:
			return trackResult{Checked: true, Findings: withoutFixes(findings)}
		case kept:
			return trackResult{Checked: true, Findings: []finding{confirmed}}
		}
		return trackResult{Checked: true, Findings: findings}
	}
	findings = append(findings, coverFindings(ctx, covers, lib, found)...)
	return trackResult{Checked: true, Findings: findings, Matched: &found}
}

func musicBrainzKeepsISRC(ctx context.Context, cat catalog, lib libraryTrack, deezer catalogTrack) (finding, bool, error) {
	recordings, ok := cat.(recordingCatalog)
	if !ok {
		return finding{}, false, errors.New("musicbrainz was not consulted")
	}
	found, err := recordings.RecordingsByISRC(ctx, lib.ISRC)
	if errors.Is(err, errCatalogUnknown) {
		return finding{}, false, nil
	}
	if err != nil {
		return finding{}, false, err
	}
	same := sameSongRecordings(lib, found)
	if len(same) == 0 {
		return finding{}, false, nil
	}
	return about(lib, categoryISRCConfirmed, "musicbrainz recording "+same[0].ID+"; deezer returned "+deezer.Title), true, nil
}

func withoutFixes(findings []finding) []finding {
	out := make([]finding, 0, len(findings))
	for _, f := range findings {
		f.Fix = nil
		out = append(out, f)
	}
	return out
}

func auditTrackWithFills(ctx context.Context, cat catalog, covers coverJudge, lib libraryTrack) trackResult {
	result := auditTrack(ctx, cat, covers, lib)
	if result.Matched == nil {
		return result
	}
	resolved, err := resolveOtherReleases(ctx, cat, lib, result.Findings)
	if err != nil {
		return lookupFailure(ctx, lib, err)
	}
	enriched, err := enrichmentFindings(ctx, cat, lib, *result.Matched)
	if err != nil {
		return lookupFailure(ctx, lib, err)
	}
	withCoverFixes, err := coverFixFindings(ctx, cat, covers, lib, *result.Matched, slices.Concat(resolved, enriched))
	if err != nil {
		return lookupFailure(ctx, lib, err)
	}
	result.Findings = withCoverFixes
	return result
}

func hasCategory(findings []finding, want category) bool {
	for _, f := range findings {
		if f.Category == want {
			return true
		}
	}
	return false
}

func ignoreUnblocked(err error) error {
	if errors.Is(err, errCatalogBlocked) || errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

func coverFindings(ctx context.Context, covers coverJudge, lib libraryTrack, found catalogTrack) []finding {
	if lib.ArtworkURL == "" || found.CoverURL == "" || coverURLContainsImage(lib.ArtworkURL, found.AlbumImageMD5) {
		return nil
	}
	findings := []finding{about(lib, categoryCoverURLDiffers, lib.ArtworkURL)}
	same, err := covers.SameImage(ctx, lib.ArtworkURL, found.CoverURL)
	switch {
	case err != nil:
		return append(findings, about(lib, categoryCoverUnreadable, err.Error()))
	case !same:
		return append(findings, about(lib, categoryCoverImageDiffers, lib.ArtworkURL+" vs "+found.CoverURL))
	}
	return findings
}

type auditOutcome struct {
	Total       int
	Checked     int
	Findings    []finding
	AbortReason string
}

func auditAll(ctx context.Context, cat catalog, covers coverJudge, tracks []libraryTrack, workers int) auditOutcome {
	outcome := auditOutcome{Total: len(tracks)}
	results, blockedErr := auditEach(ctx, cat, covers, tracks, workers)
	if blockedErr != nil {
		outcome.AbortReason = blockedErr.Error()
	}
	for _, result := range results {
		if result.Checked {
			outcome.Checked++
		}
		outcome.Findings = append(outcome.Findings, result.Findings...)
	}
	return outcome
}

func auditEach(ctx context.Context, cat catalog, covers coverJudge, tracks []libraryTrack, workers int) ([]trackResult, error) {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	results := make([]trackResult, len(tracks))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range max(workers, 1) {
		wg.Go(func() {
			for i := range jobs {
				results[i] = auditTrackWithFills(ctx, cat, covers, tracks[i])
				if results[i].Blocked != nil {
					cancel(results[i].Blocked)
				}
			}
		})
	}
	feedUntilDone(ctx, jobs, len(tracks))
	wg.Wait()
	return results, blockCause(ctx)
}

func feedUntilDone(ctx context.Context, jobs chan<- int, count int) {
	defer close(jobs)
	for i := range count {
		select {
		case jobs <- i:
		case <-ctx.Done():
			return
		}
	}
}

func blockCause(ctx context.Context) error {
	if cause := context.Cause(ctx); errors.Is(cause, errCatalogBlocked) {
		return cause
	}
	return nil
}
