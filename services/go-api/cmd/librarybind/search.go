package main

import (
	"altune/go-api/internal/catalog/domain"
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
	musicBrainzPlainAlbum  = "Album"
	musicBrainzPlainSingle = "Single"
	musicBrainzPlainEP     = "EP"
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
		released := originalRelease(rec.Releases)
		for _, isrc := range rec.ISRCs {
			c := musicBrainzCandidate(rec, isrc)
			c.Released = released
			found = append(found, c)
		}
	}
	return found, nil
}

func originalRelease(releases []musicBrainzRelease) string {
	earliest := ""
	for _, release := range releases {
		group := release.Group
		isPlain := len(group.SecondaryTypes) == 0 && slices.Contains([]string{musicBrainzPlainAlbum, musicBrainzPlainSingle, musicBrainzPlainEP}, group.PrimaryType)
		if isPlain && compareReleased(release.Date, earliest) < 0 {
			earliest = release.Date
		}
	}
	return earliest
}

func compareReleased(a, b string) int {
	switch {
	case a == "" && b == "":
		return 0
	case a == "":
		return 1
	case b == "":
		return -1
	}
	shared := min(len(a), len(b))
	return strings.Compare(a[:shared], b[:shared])
}

type searchBinder struct {
	musicBrainz recordingSearch
	deezer      isrcLookup
}

func leadArtist(artist string) string {
	return strings.TrimSpace(versiontag.FeaturedCredit.ReplaceAllString(artist, ""))
}

func matchingTrack(t libraryTrack, keys domain.SearchKeys) libraryTrack {
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

func (b searchBinder) confirmWithDeezer(ctx context.Context, match libraryTrack, proposal judged) (sourceOutcome, error) {
	match.ISRC = proposal.ISRC
	listed, err := b.deezer.ByISRC(ctx, proposal.ISRC)
	if stopsTheRun(ctx, err) {
		return sourceOutcome{}, errors.Join(err, ctx.Err())
	}
	return outcomeOf(match, b.deezer.Name(), listed, err), nil
}

func rankedProposals(fileSeconds float64, proposals sourceOutcome) []judged {
	closest := map[string]judged{}
	for _, j := range proposals.Candidates {
		isrc := songfacts.CanonicalISRC(j.ISRC)
		held, seen := closest[isrc]
		if j.isGuarded() && (!seen || lengthGap(fileSeconds, j) < lengthGap(fileSeconds, held)) {
			closest[isrc] = j
		}
	}
	ranked := make([]judged, 0, len(closest))
	for _, j := range closest {
		ranked = append(ranked, j)
	}
	slices.SortFunc(ranked, func(a, b judged) int {
		return cmp.Or(
			compareReleased(a.Released, b.Released),
			cmp.Compare(lengthGap(fileSeconds, a), lengthGap(fileSeconds, b)),
			strings.Compare(a.ISRC, b.ISRC),
		)
	})
	return ranked[:min(len(ranked), maxISRCsConfirmed)]
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
	i.Reason = reasonSearchPrefix + i.Reason
	if storedISRC := songfacts.CanonicalISRC(t.ISRC); storedISRC != "" && storedISRC != i.ISRC {
		i.Evidence.Replaced = &replacedISRC{ISRC: storedISRC, Refusal: priorReason, CatalogSeconds: winningCandidate(confirmation).LengthSeconds}
	}
	return i
}
