package discoverybridge

import (
	"altune/go-api/internal/shared/versiontag"
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strconv"

	acqports "altune/go-api/internal/acquisition/ports"
	discoveryports "altune/go-api/internal/discovery/ports"
)

const (
	maxRerecordingLookups = 5
	officialReleaseStatus = "Official"
)

var _ acqports.RerecordingFinder = (*RecordingResolver)(nil)

type rerecordingAuthority interface {
	WorkRecordings(ctx context.Context, recordingMBID string) ([]discoveryports.MBWorkRecording, error)
	RecordingDetail(ctx context.Context, mbid string) (discoveryports.MBRecordingDetail, bool, error)
}

func WithRerecordingAuthority(a rerecordingAuthority) func(*RecordingResolver) {
	return func(r *RecordingResolver) { r.rerecording = a }
}

func (r *RecordingResolver) FindRerecording(ctx context.Context, q acqports.RerecordingQuery) (acqports.RecordingLabel, bool, error) {
	if r.rerecording == nil {
		return acqports.RecordingLabel{}, false, nil
	}
	original, found, err := r.rerecording.RecordingDetail(ctx, q.OriginalMBID)
	if err != nil {
		return acqports.RecordingLabel{}, false, fmt.Errorf("rerecording original %s: %w", q.OriginalMBID, err)
	}
	if !found {
		slog.InfoContext(ctx, "acquisition.rerecording_none", "original_mbid", q.OriginalMBID, "candidates", 0)
		return acqports.RecordingLabel{}, false, nil
	}
	return r.rerecordingOf(ctx, original)
}

func (r *RecordingResolver) rerecordingOf(ctx context.Context, original discoveryports.MBRecordingDetail) (acqports.RecordingLabel, bool, error) {
	work, err := r.rerecording.WorkRecordings(ctx, original.MBID)
	if err != nil {
		return acqports.RecordingLabel{}, false, fmt.Errorf("rerecording work of %s: %w", original.MBID, err)
	}
	candidates := labelledRerecordings(original, work)
	details, err := r.releasedAfter(ctx, original, candidates)
	if err != nil {
		return acqports.RecordingLabel{}, false, err
	}
	if len(details) == 0 {
		slog.InfoContext(ctx, "acquisition.rerecording_none", "original_mbid", original.MBID, "candidates", len(candidates))
		return acqports.RecordingLabel{}, false, nil
	}
	chosen := preferredRerecording(details)
	slog.InfoContext(ctx, "acquisition.rerecording_found",
		"original_mbid", original.MBID, "rerecording_mbid", chosen.MBID, "candidates", len(candidates))
	return rerecordingLabel(chosen), true, nil
}

func labelledRerecordings(original discoveryports.MBRecordingDetail, work []discoveryports.MBWorkRecording) []discoveryports.MBWorkRecording {
	songTitle := versiontag.WithoutVersionPhrases(versiontag.Tokens(original.Title))
	var kept []discoveryports.MBWorkRecording
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

func rerecordingSongTitle(rec discoveryports.MBWorkRecording) ([]string, bool) {
	title, inTitle := versiontag.WithoutRerecordingPhrases(versiontag.Tokens(rec.Title))
	disambiguation, inDisambiguation := versiontag.WithoutRerecordingPhrases(versiontag.Tokens(rec.Disambiguation))
	if !inTitle && !inDisambiguation {
		return nil, false
	}
	if vetoed(title) || vetoed(disambiguation) {
		return nil, false
	}
	return versiontag.WithoutVersionPhrases(title), true
}

func vetoed(tokens []string) bool {
	return slices.ContainsFunc(versiontag.Matches(tokens), func(e versiontag.Entry) bool { return e.Veto })
}

func (r *RecordingResolver) releasedAfter(ctx context.Context, original discoveryports.MBRecordingDetail, candidates []discoveryports.MBWorkRecording) ([]discoveryports.MBRecordingDetail, error) {
	var kept []discoveryports.MBRecordingDetail
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

func earliestOfficialRelease(releases []discoveryports.MBRelease) (discoveryports.MBRelease, bool) {
	var earliest discoveryports.MBRelease
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

func preferredRerecording(details []discoveryports.MBRecordingDetail) discoveryports.MBRecordingDetail {
	ranked := slices.Clone(details)
	if len(ranked) == 0 {
		return discoveryports.MBRecordingDetail{}
	}
	slices.SortStableFunc(ranked, func(a, b discoveryports.MBRecordingDetail) int {
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

func rerecordingLabel(detail discoveryports.MBRecordingDetail) acqports.RecordingLabel {
	release, _ := earliestOfficialRelease(detail.Releases)
	label := acqports.RecordingLabel{
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
