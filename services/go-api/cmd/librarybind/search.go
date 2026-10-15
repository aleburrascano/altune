package main

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared/releasepick"
	"altune/go-api/internal/shared/songfacts"
	"altune/go-api/internal/shared/versiontag"
	"cmp"
	"context"
	"errors"
	"math"
	"slices"
	"strings"
)

const (
	reasonSearchPrefix     = "search: "
	musicBrainzSearchLimit = 100
	maxISRCsConfirmed      = 5
)

var luceneQuoteEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`)

type recordingSearch interface {
	Name() source
	Search(ctx context.Context, artist, title string) ([]candidate, error)
}

func (m musicBrainzLookup) Search(ctx context.Context, artist, title string) ([]candidate, error) {
	query := `recording:"` + luceneQuoteEscaper.Replace(title) + `" AND artist:"` + luceneQuoteEscaper.Replace(artist) + `" AND isrc:*`
	recordings, err := m.recordings(ctx, query, musicBrainzSearchLimit)
	if err != nil {
		return nil, err
	}
	var found []candidate
	for _, rec := range recordings {
		homes := releaseHomes(rec.Releases)
		for _, isrc := range rec.ISRCs {
			c := musicBrainzCandidate(rec, isrc)
			c.Disambiguation, c.Homes = rec.Disambiguation, homes
			found = append(found, c)
		}
	}
	return found, nil
}

func releaseHomes(releases []musicBrainzRelease) []releasepick.Group {
	homes := make([]releasepick.Group, 0, len(releases))
	for _, release := range releases {
		homes = append(homes, releasepick.Group{PrimaryType: release.Group.PrimaryType, SecondaryTypes: release.Group.SecondaryTypes, Date: release.Date})
	}
	return homes
}

type searchBinder struct {
	musicBrainz recordingSearch
	deezer      isrcLookup
	cuts        cutPolicy
}

func leadArtist(artist string) string {
	return strings.TrimSpace(versiontag.FeaturedCredit.ReplaceAllString(artist, ""))
}

func matchingTrack(t libraryTrack, keys domain.SearchKeys) libraryTrack {
	t.Artist = keys.Artist
	if len(keys.VariantQualifiers()) == 0 {
		t.Title = keys.Title
	}
	return t
}

func searchOutcome(t libraryTrack, from source, found []candidate, searchErr error) sourceOutcome {
	outcome := sourceOutcome{Source: from, Candidates: []judged{}}
	if searchErr != nil {
		outcome.Error = searchErr.Error()
		return outcome
	}
	for _, c := range found {
		if songfacts.CanonicalISRC(c.ISRC) != "" {
			outcome.Candidates = append(outcome.Candidates, ignoringISRC(t, c))
		}
	}
	return outcome
}

func (b searchBinder) refine(ctx context.Context, t libraryTrack, found identity) (identity, error) {
	keys := domain.CleanSearchKeys(t.Title, t.Artist, "")
	artist := leadArtist(keys.Artist)
	if !wantsSearch(found) || artist == "" || keys.Title == "" {
		return found, nil
	}
	match := matchingTrack(t, keys)
	listed, err := b.musicBrainz.Search(ctx, artist, keys.Title)
	if stopsTheRun(ctx, err) {
		return found, errors.Join(err, ctx.Err())
	}
	proposals := searchOutcome(match, b.musicBrainz.Name(), listed, err)
	found.Evidence.Search = []sourceOutcome{proposals}
	return b.confirmFirstProposal(ctx, t, match, rankedProposals(match.FileSeconds, proposals), found)
}

func (b searchBinder) confirmFirstProposal(ctx context.Context, t, match libraryTrack, ranked []judged, found identity) (identity, error) {
	for _, proposal := range ranked {
		confirmation, err := b.confirmWithDeezer(ctx, match, proposal)
		if err != nil {
			return found, err
		}
		found.Evidence.Search = append(found.Evidence.Search, confirmation)
		if confirmation.Winner != "" && !confirmation.Ambiguous {
			return found.boundBySearch(t, proposal, confirmation), nil
		}
	}
	return found, nil
}

func (b searchBinder) refineDifferingCut(ctx context.Context, t libraryTrack, found identity) (identity, error) {
	if !wantsSearch(found) || len(found.Evidence.Search) == 0 || found.hasUnlistedRecordingAtFileLength(found.Evidence.Search[0].Candidates) {
		return found, nil
	}
	match := matchingTrack(t, domain.CleanSearchKeys(t.Title, t.Artist, ""))
	for _, proposal := range rankedDifferingCuts(match.FileSeconds, found.Evidence.Search[0]) {
		if !b.cuts.allows(lengthGap(match.FileSeconds, proposal)) {
			found.refuseCut(match.FileSeconds, proposal.LengthSeconds)
			continue
		}
		confirmation, err := b.confirmAtCatalogLength(ctx, match, proposal)
		if err != nil {
			return found, err
		}
		found.Evidence.Search = append(found.Evidence.Search, confirmation)
		if confirmation.Winner != "" && !confirmation.Ambiguous {
			return found.boundBySearch(match, proposal, confirmation), nil
		}
	}
	return found, nil
}

func (b searchBinder) confirmAtCatalogLength(ctx context.Context, match libraryTrack, proposal judged) (sourceOutcome, error) {
	match.FileSeconds = proposal.LengthSeconds
	return b.confirmWithDeezer(ctx, match, proposal)
}

func (b searchBinder) confirmWithDeezer(ctx context.Context, match libraryTrack, proposal judged) (sourceOutcome, error) {
	match.ISRC = proposal.ISRC
	listed, err := b.deezer.ByISRC(ctx, proposal.ISRC)
	if stopsTheRun(ctx, err) {
		return sourceOutcome{}, errors.Join(err, ctx.Err())
	}
	return outcomeOf(match, b.deezer.Name(), listed, err), nil
}

func rankedProposals(fileSeconds float64, proposals sourceOutcome) []judged {
	return rankedOriginals(fileSeconds, proposals, judged.isGuarded)
}

func rankedDifferingCuts(fileSeconds float64, proposals sourceOutcome) []judged {
	return rankedOriginals(fileSeconds, proposals, isRefusedOnlyForLength)
}

func rankedOriginals(fileSeconds float64, proposals sourceOutcome, isProposable func(judged) bool) []judged {
	closest := map[string]judged{}
	for _, j := range proposals.Candidates {
		isrc := songfacts.CanonicalISRC(j.ISRC)
		held, seen := closest[isrc]
		if isProposable(j) && j.isOriginalRecording() && (!seen || lengthGap(fileSeconds, j) < lengthGap(fileSeconds, held)) {
			closest[isrc] = j
		}
	}
	ranked := make([]judged, 0, len(closest))
	for _, j := range closest {
		ranked = append(ranked, j)
	}
	slices.SortFunc(ranked, func(a, b judged) int { return compareProposals(fileSeconds, a, b) })
	return ranked[:min(len(ranked), maxISRCsConfirmed)]
}

func compareProposals(fileSeconds float64, a, b judged) int {
	return cmp.Or(
		cmp.Compare(originalKind(a), originalKind(b)),
		releasepick.CompareDates(originalDate(a), originalDate(b)),
		cmp.Compare(lengthGap(fileSeconds, a), lengthGap(fileSeconds, b)),
		strings.Compare(a.ISRC, b.ISRC),
	)
}

func (j judged) isOriginalRecording() bool {
	_, _, ok := j.original()
	return ok
}

func originalKind(j judged) releasepick.Kind {
	_, kind, _ := j.original()
	return kind
}

func originalDate(j judged) string {
	home, _, _ := j.original()
	return home.Date
}

func lengthGap(fileSeconds float64, j judged) float64 {
	return math.Abs(j.LengthSeconds - fileSeconds)
}

func (i identity) boundBySearch(t libraryTrack, proposal judged, confirmation sourceOutcome) identity {
	priorReason := i.Reason
	proposed := sourceOutcome{Source: sourceMusicBrainz, Winner: proposal.ID}
	i.Status = statusBound
	i.ISRC = songfacts.CanonicalISRC(proposal.ISRC)
	i.recordConfirmations([]sourceOutcome{proposed, confirmation})
	i.noteLengthGap(t.FileSeconds, proposal.LengthSeconds)
	i.Reason = reasonSearchPrefix + i.Reason
	if storedISRC := songfacts.CanonicalISRC(t.ISRC); storedISRC != "" && storedISRC != i.ISRC {
		i.Evidence.Replaced = &replacedISRC{ISRC: storedISRC, Refusal: priorReason, CatalogSeconds: winningCandidate(confirmation).LengthSeconds}
	}
	return i
}
