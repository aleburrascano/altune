package musicbrainzmatch

import (
	"altune/go-api/internal/shared/titlemarkers"
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strconv"

	acqports "altune/go-api/internal/download/internal/core/ports"
)

const (
	maxRerecordingLookups = 5
	officialReleaseStatus = "Official"
)

var _ acqports.RerecordingFinder = (*Resolver)(nil)

func WithRerecordingLookup(a acqports.RerecordingLookup) func(*Resolver) {
	return func(r *Resolver) { r.rerecording = a }
}

func (r *Resolver) FindRerecording(ctx context.Context, q acqports.RerecordingQuery) (acqports.TrackLabel, bool, error) {
	if r.rerecording == nil {
		return acqports.TrackLabel{}, false, nil
	}
	original, found, err := r.rerecording.RecordingDetail(ctx, q.OriginalMBID)
	if err != nil {
		return acqports.TrackLabel{}, false, fmt.Errorf("rerecording original %s: %w", q.OriginalMBID, err)
	}
	if !found {
		slog.InfoContext(ctx, "acquisition.rerecording_none", "original_mbid", q.OriginalMBID, "candidates", 0)
		return acqports.TrackLabel{}, false, nil
	}
	return r.rerecordingOf(ctx, original)
}

func (r *Resolver) rerecordingOf(ctx context.Context, original acqports.RecordingDetail) (acqports.TrackLabel, bool, error) {
	work, err := r.rerecording.WorkRecordings(ctx, original.MBID)
	if err != nil {
		return acqports.TrackLabel{}, false, fmt.Errorf("rerecording work of %s: %w", original.MBID, err)
	}
	candidates := labelledRerecordings(original, work)
	details, err := r.releasedAfter(ctx, original, candidates)
	if err != nil {
		return acqports.TrackLabel{}, false, err
	}
	if len(details) == 0 {
		slog.InfoContext(ctx, "acquisition.rerecording_none", "original_mbid", original.MBID, "candidates", len(candidates))
		return acqports.TrackLabel{}, false, nil
	}
	chosen := preferredRerecording(details)
	slog.InfoContext(ctx, "acquisition.rerecording_found",
		"original_mbid", original.MBID, "rerecording_mbid", chosen.MBID, "candidates", len(candidates))
	return rerecordingLabel(chosen), true, nil
}

func labelledRerecordings(original acqports.RecordingDetail, work []acqports.WorkRecording) []acqports.WorkRecording {
	songTitle := titlemarkers.WithoutVersionPhrases(titlemarkers.Tokens(original.Title))
	var kept []acqports.WorkRecording
	for _, rec := range work {
		if rec.MBID == original.MBID || rec.Live || !sameLeadArtist(rec.ArtistIDs, original.ArtistIDs) {
			continue
		}
		if title, ok := rerecordingSongTitle(rec); ok && slices.Equal(title, songTitle) {
			kept = append(kept, rec)
		}
	}
	return kept
}

func sameLeadArtist(a, b []string) bool {
	return len(a) > 0 && len(b) > 0 && a[0] == b[0]
}

func rerecordingSongTitle(rec acqports.WorkRecording) ([]string, bool) {
	title, inTitle := titlemarkers.WithoutRerecordingPhrases(titlemarkers.Tokens(rec.Title))
	disambiguation, inDisambiguation := titlemarkers.WithoutRerecordingPhrases(titlemarkers.Tokens(rec.Disambiguation))
	if !inTitle && !inDisambiguation {
		return nil, false
	}
	if vetoed(title) || vetoed(disambiguation) {
		return nil, false
	}
	return titlemarkers.WithoutVersionPhrases(title), true
}

func vetoed(tokens []string) bool {
	return slices.ContainsFunc(titlemarkers.Matches(tokens), func(e titlemarkers.Entry) bool { return e.Veto })
}

func (r *Resolver) releasedAfter(ctx context.Context, original acqports.RecordingDetail, candidates []acqports.WorkRecording) ([]acqports.RecordingDetail, error) {
	var kept []acqports.RecordingDetail
	for _, rec := range candidates[:min(len(candidates), maxRerecordingLookups)] {
		detail, found, err := r.rerecording.RecordingDetail(ctx, rec.MBID)
		if err != nil {
			return nil, fmt.Errorf("rerecording candidate %s: %w", rec.MBID, err)
		}
		if !found || detail.FirstReleaseDate <= original.FirstReleaseDate {
			continue
		}
		if _, official := earliestOfficialRelease(detail.Releases); official {
			kept = append(kept, detail)
		}
	}
	return kept, nil
}

func earliestOfficialRelease(releases []acqports.RecordingRelease) (acqports.RecordingRelease, bool) {
	var earliest acqports.RecordingRelease
	found := false
	for _, rel := range releases {
		if rel.Status != officialReleaseStatus {
			continue
		}
		if !found || releaseDateBefore(rel.Date, earliest.Date) {
			earliest, found = rel, true
		}
	}
	return earliest, found
}

func releaseDateBefore(a, b string) bool {
	if a == "" || b == "" {
		return b == "" && a != ""
	}
	return a < b
}

func preferredRerecording(details []acqports.RecordingDetail) acqports.RecordingDetail {
	ranked := slices.Clone(details)
	if len(ranked) == 0 {
		return acqports.RecordingDetail{}
	}
	slices.SortStableFunc(ranked, func(a, b acqports.RecordingDetail) int {
		if hasA, hasB := len(a.ISRCs) > 0, len(b.ISRCs) > 0; hasA != hasB {
			if hasA {
				return -1
			}
			return 1
		}
		return cmp.Compare(a.FirstReleaseDate, b.FirstReleaseDate)
	})
	return ranked[0]
}

func rerecordingLabel(detail acqports.RecordingDetail) acqports.TrackLabel {
	release, _ := earliestOfficialRelease(detail.Releases)
	label := acqports.TrackLabel{
		MBID:            detail.MBID,
		Title:           detail.Title,
		Album:           release.Title,
		Year:            releaseYear(release.Date),
		DurationSeconds: detail.Duration,
	}
	if len(detail.ISRCs) > 0 {
		label.ISRC = detail.ISRCs[0]
	}
	return label
}

func releaseYear(date string) int {
	if len(date) < 4 {
		return 0
	}
	year, err := strconv.Atoi(date[:4])
	if err != nil {
		return 0
	}
	return year
}
