package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"strconv"
)

type skipped struct {
	Category category
	Detail   string
}

func (s skipped) Error() string { return string(s.Category) + ": " + s.Detail }

type deriver struct {
	recordings recordingSource
	checker    crossChecker
}

func newDeriver(recordings recordingSource, deezer deezerCatalog) *deriver {
	return &deriver{recordings: recordings, checker: crossChecker{deezer: deezer}}
}

type derivation struct {
	Findings []finding
	Done     int
	Aborted  error
}

func (d *deriver) deriveAll(ctx context.Context, tracks []boundTrack) derivation {
	var out derivation
	for _, t := range tracks {
		found, err := d.deriveTrack(ctx, t)
		if errors.Is(err, errCatalogBlocked) || ctx.Err() != nil {
			out.Aborted = fmt.Errorf("aborted after %d of %d tracks: %w", out.Done, len(tracks), errors.Join(err, ctx.Err()))
			return out
		}
		out.Done++
		out.Findings = append(out.Findings, found...)
	}
	return out
}

func (d *deriver) deriveTrack(ctx context.Context, t boundTrack) ([]finding, error) {
	found, err := d.findingsFor(ctx, t)
	var skip skipped
	switch {
	case errors.As(err, &skip):
		return []finding{about(t, skip.Category, skip.Detail)}, nil
	case errors.Is(err, errCatalogBlocked) || ctx.Err() != nil:
		return nil, err
	case err != nil:
		return []finding{about(t, categoryLookupFailed, err.Error())}, nil
	}
	return found, nil
}

func (d *deriver) findingsFor(ctx context.Context, t boundTrack) ([]finding, error) {
	rec, err := d.recordingOf(ctx, t)
	var unknown skipped
	if errors.As(err, &unknown) && unknown.Category == categoryRecordingUnknown {
		return d.deezerOnlyFindings(ctx, t, unknown)
	}
	if err != nil {
		return nil, err
	}
	chosen, err := d.chooseFor(ctx, rec)
	if err != nil {
		return nil, err
	}
	a := anchor{ISRC: cmp.Or(t.IdentityISRC, t.ISRC), Title: rec.Title, Artist: rec.Artist}
	check, err := d.crossCheckOf(ctx, rec, a, chosen)
	if err != nil {
		return nil, err
	}
	head := about(t, categoryReleaseChosen, chosen.Group.describe()+" by "+string(chosen.Rule)+"; deezer "+string(check.Outcome))
	return append([]finding{head}, changesFor(t, a, chosen, check)...), nil
}

func (d *deriver) recordingOf(ctx context.Context, t boundTrack) (recording, error) {
	id := t.MBRecordingID
	if id == "" {
		var err error
		if id, err = d.recordingIDByISRC(ctx, cmp.Or(t.IdentityISRC, t.ISRC)); err != nil {
			return recording{}, err
		}
	}
	rec, err := d.recordings.Recording(ctx, id)
	if errors.Is(err, errCatalogUnknown) {
		return recording{}, skipped{categoryRecordingUnknown, "musicbrainz has no recording " + id}
	}
	return rec, err
}

func (d *deriver) recordingIDByISRC(ctx context.Context, isrc string) (string, error) {
	if isrc == "" {
		return "", skipped{categoryNoAnchor, "bound without an mb_recording_id or an isrc"}
	}
	found, err := d.recordings.RecordingsByISRC(ctx, isrc)
	if err != nil && !errors.Is(err, errCatalogUnknown) {
		return "", err
	}
	var ids []string
	for _, rec := range found {
		if rec.hasISRC(isrc) {
			ids = append(ids, rec.ID)
		}
	}
	switch len(ids) {
	case 0:
		return "", skipped{categoryRecordingUnknown, "musicbrainz has no recording with isrc " + isrc}
	case 1:
		return ids[0], nil
	}
	return "", skipped{categoryRecordingAmbiguous, strconv.Itoa(len(ids)) + " musicbrainz recordings carry isrc " + isrc}
}

func (d *deriver) chooseFor(ctx context.Context, rec recording) (pick, error) {
	groups, err := d.recordings.ReleaseGroups(ctx, rec.ID)
	if err != nil && !errors.Is(err, errCatalogUnknown) {
		return pick{}, err
	}
	chosen, ok := chooseRelease(ownSingles(groups, rec.Title))
	if !ok {
		return pick{}, skipped{categoryReleaseUnchosen, strconv.Itoa(len(groups)) + " release groups, none an original album or single"}
	}
	return chosen, nil
}

func (d *deriver) crossCheckOf(ctx context.Context, rec recording, a anchor, chosen pick) (crossCheck, error) {
	if a.ISRC == "" {
		return crossCheck{Outcome: outcomeNoAlbum}, nil
	}
	return d.checker.check(ctx, crossCheckInput{
		ISRC: a.ISRC, Artist: cmp.Or(rec.Primary, rec.Artist), SongTitle: rec.Title, Group: chosen.Group, RecordingISRCs: rec.ISRCs,
	})
}

func changesFor(t boundTrack, a anchor, chosen pick, check crossCheck) []finding {
	if check.Outcome == outcomeOtherMaster {
		return otherMasterFindings(t, a, chosen, *check.OtherMaster)
	}
	return differenceFindings(t, a, chosen.Group, check.Match)
}

func differenceFindings(t boundTrack, a anchor, group releaseGroup, match *deezerMatch) []finding {
	var found []finding
	for _, d := range diff(t.libraryTrack, derive(a, group, match)) {
		found = append(found, findingOf(t, d))
	}
	return found
}

func otherMasterFindings(t boundTrack, a anchor, chosen pick, master deezerTrack) []finding {
	var found []finding
	for _, d := range diff(t.libraryTrack, derive(a, chosen.Group, nil)) {
		if d.Mode == modeReportOnly {
			found = append(found, findingOf(t, d))
		}
	}
	detail := "isrc " + t.ISRC + " -> " + master.ISRC + " on " + master.AlbumTitle + ", " + strconv.Itoa(int(master.DurationSeconds)) + "s for the " + strconv.Itoa(int(t.FileSeconds)) + "s file"
	if t.ISRC == "" || !fitsFile(t.FileSeconds, master.DurationSeconds) {
		return append(found, about(t, categoryOtherMasterUnfit, detail))
	}
	plan := about(t, categoryISRCOtherMaster, detail)
	plan.LengthPlan = &lengthPlan{
		Action: actionFixISRC, UserID: t.UserID, Version: t.Version, OldISRC: t.ISRC, NewISRC: canonicalISRC(master.ISRC),
		FileSeconds: t.FileSeconds, CatalogSeconds: master.DurationSeconds,
	}
	return append(found, plan)
}
