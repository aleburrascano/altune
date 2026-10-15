package main

import (
	"altune/go-api/internal/shared/songfacts"
	"cmp"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const coverArtBaseURL = "https://coverartarchive.org"

type skipped struct {
	Category category
	Detail   string
}

func (s skipped) Error() string { return string(s.Category) + ": " + s.Detail }

type deriver struct {
	recordings recordingSource
	checker    crossChecker
	covers     coverProbe
}

func newDeriver(recordings recordingSource, deezer deezerCatalog) *deriver {
	return &deriver{recordings: recordings, checker: crossChecker{deezer: deezer}, covers: trustedCovers{}}
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
	t = d.withCoverVerdict(ctx, t)
	rec, err := d.recordingOf(ctx, t)
	var unknown skipped
	if errors.As(err, &unknown) && unknown.Category == categoryRecordingUnknown {
		return d.deezerOnlyFindings(ctx, t, unknown)
	}
	if err != nil {
		return nil, err
	}
	chosen, homes, err := d.chooseFor(ctx, rec)
	if err != nil {
		return nil, err
	}
	a := anchor{ISRC: cmp.Or(t.IdentityISRC, t.ISRC), Title: rec.Title, Artist: rec.Artist}
	check, err := d.crossCheckOf(ctx, rec, a, chosen)
	if err != nil {
		return nil, err
	}
	own, err := d.withOwnRelease(ctx, rec, chosen.Group, check.Match)
	if err != nil {
		return nil, err
	}
	if !isProvenISRC(rec, t, a.ISRC) {
		own.Withheld.ISRC = "the isrc is not listed on the musicbrainz recording and bind did not confirm it on deezer or apple"
		a.ISRC = ""
	}
	own.Withheld.Bind = bindHold(a.Artist, check.Track)
	if own.Withheld.Album, err = d.heldAlbumReason(ctx, t, rec, a, chosen.Group, homes, check, own.Withheld.Bind); err != nil {
		return nil, err
	}
	own.Withheld.Title = titleHold(a.Title, check.Track)
	own.Withheld.Artist = artistHold(a.Artist, check.Track)
	check.Match = own
	head := about(t, categoryReleaseChosen, chosen.Group.describe()+" by "+string(chosen.Rule)+"; deezer "+string(check.Outcome))
	return append([]finding{head}, changesFor(t, a, chosen, check)...), nil
}

func (d *deriver) heldAlbumReason(ctx context.Context, t boundTrack, rec recording, a anchor, chosen releaseGroup, homes []releaseGroup, check crossCheck, bindDoubt string) (string, error) {
	if reason := albumHold(t.Album, chosen, homes, check, rec.leadArtist()); reason != "" || bindDoubt != "" || t.Album == "" || a.ISRC == "" || !albumDiffers(t.Album, chosen.Title) {
		return reason, nil
	}
	in := crossCheckInput{ISRC: a.ISRC, Artist: rec.leadArtist(), SongTitle: rec.Title, RecordingISRCs: rec.ISRCs}
	listing, err := d.checker.storedAlbumListing(ctx, in, t.Album)
	switch {
	case err != nil || listing == nil:
		return "", err
	case isEarlier(listing.ReleaseDate, chosen.FirstReleaseDate):
		return reasonStoredListsTrackEarlier, nil
	case datesOnlyItsYear(chosen.FirstReleaseDate, listing.ReleaseDate):
		return reasonStoredTiesChosenYear, nil
	}
	return "", nil
}

func datesOnlyItsYear(chosenDate, listedDate string) bool {
	return len(chosenDate) == yearDigits && strings.HasPrefix(listedDate, chosenDate)
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

func (d *deriver) chooseFor(ctx context.Context, rec recording) (pick, []releaseGroup, error) {
	groups, err := d.recordings.ReleaseGroups(ctx, rec.ID)
	if err != nil && !errors.Is(err, errCatalogUnknown) {
		return pick{}, nil, err
	}
	chosen, ok := chooseRelease(ownSingles(groups, rec.Title))
	if !ok {
		return pick{}, nil, skipped{categoryReleaseUnchosen, strconv.Itoa(len(groups)) + " release groups, none an original album or single"}
	}
	return chosen, groups, nil
}

func (d *deriver) crossCheckOf(ctx context.Context, rec recording, a anchor, chosen pick) (crossCheck, error) {
	if a.ISRC == "" {
		return crossCheck{Outcome: outcomeNoAlbum}, nil
	}
	return d.checker.check(ctx, crossCheckInput{
		ISRC: a.ISRC, Artist: rec.leadArtist(), SongTitle: rec.Title, Group: chosen.Group, RecordingISRCs: rec.ISRCs,
	})
}

func (d *deriver) withOwnRelease(ctx context.Context, rec recording, group releaseGroup, match *deezerMatch) (*deezerMatch, error) {
	own := deezerMatch{}
	if match != nil {
		own = *match
	}
	own.Withheld = withheld{AlbumArtist: albumArtistHold(group.Artist, match)}
	own.CoverURL, own.Withheld.ArtworkURL = d.ownCover(ctx, own.CoverURL, group)
	listed, err := d.listedTrack(ctx, rec, group, match)
	if match == nil {
		own.TrackPosition = listed.Position
		own.Withheld.Genre = reasonNoDeezerAlbum
		if listed.Position == 0 {
			own.Withheld.TrackNumber = reasonNoDeezerAlbum
		}
	}
	if own.DurationSeconds = cmp.Or(own.DurationSeconds, listed.Seconds); own.DurationSeconds == 0 {
		own.Withheld.CatalogDuration = "no track length on the chosen release"
	}
	return &own, err
}

func (d *deriver) listedTrack(ctx context.Context, rec recording, group releaseGroup, match *deezerMatch) (releaseTrack, error) {
	releaseID := cmp.Or(group.FrontReleaseID, group.ReleaseID)
	if match != nil && match.DurationSeconds != 0 || releaseID == "" {
		return releaseTrack{}, nil
	}
	listed, err := d.recordings.ReleaseTrack(ctx, releaseID, rec.ID)
	if errors.Is(err, errCatalogBlocked) {
		return releaseTrack{}, err
	}
	return listed, nil
}

type coverVerdict int

const (
	coverGenuine coverVerdict = iota
	coverBlank
	coverUnchecked
)

func (d *deriver) verdictOf(ctx context.Context, coverURL string) coverVerdict {
	switch placeholder, err := d.covers.IsPlaceholder(ctx, coverURL); {
	case coverURL == "":
		return coverGenuine
	case strings.Contains(coverURL, deezerPlaceholderImageID):
		return coverBlank
	case err != nil:
		return coverUnchecked
	case placeholder:
		return coverBlank
	}
	return coverGenuine
}

func (d *deriver) withCoverVerdict(ctx context.Context, t boundTrack) boundTrack {
	switch d.verdictOf(ctx, t.ArtworkURL) {
	case coverBlank:
		t.CoverIsPlaceholder = true
	case coverUnchecked:
		t.CoverCheckFailed = "the stored cover could not be checked: the cover lookup failed"
	}
	return t
}

func isProvenISRC(rec recording, t boundTrack, isrc string) bool {
	return songfacts.CanonicalISRC(isrc) == songfacts.CanonicalISRC(t.ISRC) || rec.hasISRC(isrc) || t.CatalogConfirmed
}

func (d *deriver) ownCover(ctx context.Context, deezerCover string, group releaseGroup) (string, string) {
	verdict := d.verdictOf(ctx, deezerCover)
	if deezerCover != "" && verdict == coverGenuine {
		return deezerCover, ""
	}
	if verdict == coverUnchecked {
		return "", "the deezer cover could not be checked: the cover lookup failed"
	}
	if group.FrontReleaseID != "" {
		if cover := d.resolvingCover(ctx, coverArtFrontURL(group.FrontReleaseID)); cover != "" {
			return cover, ""
		}
		return "", "the cover art archive front of the chosen release does not resolve"
	}
	if verdict == coverBlank {
		return "", "deezer cover is the blank placeholder and the chosen release has no cover art archive front"
	}
	return "", "the chosen release has no cover"
}

func (d *deriver) resolvingCover(ctx context.Context, coverURL string) string {
	if resolves, err := d.covers.Resolves(ctx, coverURL); err != nil || !resolves {
		return ""
	}
	return coverURL
}

func coverArtFrontURL(releaseID string) string {
	return coverArtBaseURL + "/release/" + releaseID + "/front"
}

func changesFor(t boundTrack, a anchor, chosen pick, check crossCheck) []finding {
	if check.Outcome == outcomeOtherMaster {
		return otherMasterWith(t, a, chosen, check.Match, *check.OtherMaster)
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
	return otherMasterWith(t, a, chosen, nil, master)
}

func otherMasterWith(t boundTrack, a anchor, chosen pick, own *deezerMatch, master deezerTrack) []finding {
	found := differenceFindings(t, a, chosen.Group, own)
	detail := "isrc " + t.ISRC + " -> " + master.ISRC + " on " + master.AlbumTitle + ", " + strconv.Itoa(int(master.DurationSeconds)) + "s for the " + strconv.Itoa(int(t.FileSeconds)) + "s file"
	if t.ISRC == "" || !fitsFile(t.FileSeconds, master.DurationSeconds) {
		return append(found, about(t, categoryOtherMasterUnfit, detail))
	}
	plan := about(t, categoryISRCOtherMaster, detail)
	plan.LengthPlan = &lengthPlan{
		Action: actionFixISRC, UserID: t.UserID, Version: t.Version, OldISRC: t.ISRC, NewISRC: songfacts.CanonicalISRC(master.ISRC),
		FileSeconds: t.FileSeconds, CatalogSeconds: master.DurationSeconds,
	}
	return append(found, plan)
}
