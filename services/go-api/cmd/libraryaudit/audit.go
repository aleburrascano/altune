package main

import (
	"context"
	"errors"
	"strconv"
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
		fillable, err := isrcFromSearch(ctx, cat, lib)
		if err != nil {
			return lookupFailure(ctx, lib, err)
		}
		return trackResult{Findings: append([]finding{about(lib, categoryNoISRC, "")}, fillable...)}
	}
	found, err := cat.TrackByISRC(ctx, lib.ISRC)
	switch {
	case errors.Is(err, errCatalogUnknown):
		return trackResult{Findings: []finding{about(lib, categoryUnknownISRC, lib.ISRC)}}
	case err != nil:
		return lookupFailure(ctx, lib, err)
	}
	findings := metadataFindings(lib, found)
	if !hasCategory(findings, categoryWrongSong) {
		findings = append(findings, coverFindings(ctx, covers, lib, found)...)
	}
	return trackResult{Checked: true, Findings: findings}
}

func hasCategory(findings []finding, want category) bool {
	for _, f := range findings {
		if f.Category == want {
			return true
		}
	}
	return false
}

func isrcFromSearch(ctx context.Context, cat catalog, lib libraryTrack) ([]finding, error) {
	candidates, err := cat.Search(ctx, lib.Artist, lib.Title)
	if err != nil {
		return nil, ignoreUnblocked(err)
	}
	fitting := fittingCandidates(lib, candidates)
	switch len(fitting) {
	case 0:
		return nil, nil
	case 1:
		return isrcOf(ctx, cat, lib, fitting[0])
	default:
		return []finding{about(lib, categoryISRCAmbiguous, ambiguousDetail(fitting))}, nil
	}
}

func fittingCandidates(lib libraryTrack, candidates []catalogTrack) []catalogTrack {
	var fitting []catalogTrack
	for _, candidate := range candidates {
		if isSameSong(lib, candidate) && fitsLength(lib, candidate) {
			fitting = append(fitting, candidate)
		}
	}
	return fitting
}

func ambiguousDetail(fitting []catalogTrack) string {
	ids := make([]string, 0, len(fitting))
	for _, candidate := range fitting {
		ids = append(ids, strconv.FormatInt(candidate.ID, 10))
	}
	return "deezer track ids " + strings.Join(ids, ", ")
}

func isrcOf(ctx context.Context, cat catalog, lib libraryTrack, candidate catalogTrack) ([]finding, error) {
	full, err := cat.TrackByID(ctx, candidate.ID)
	if err != nil {
		return nil, ignoreUnblocked(err)
	}
	if full.ISRC == "" {
		return nil, nil
	}
	return []finding{about(lib, categoryISRCFillable, full.ISRC)}, nil
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
				results[i] = auditTrack(ctx, cat, covers, tracks[i])
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
