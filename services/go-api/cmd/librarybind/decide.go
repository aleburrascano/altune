package main

import (
	"cmp"
	"math"
	"slices"
	"strings"
)

type status string

const (
	statusBound      status = "bound"
	statusUnofficial status = "unofficial"
	statusUnbound    status = "unbound"
)

const (
	reasonNoISRC          = "no-isrc"
	reasonISRCNotFound    = "isrc-not-found"
	reasonLookupFailed    = "lookup-failed"
	reasonAmbiguous       = "ambiguous"
	reasonAlbumUnreleased = "album-unreleased"
	reasonRefusedPrefix   = "refused-"
	reasonBoundPrefix     = "isrc confirmed by "
)

type sourceOutcome struct {
	Source     source   `json:"source"`
	Candidates []judged `json:"candidates"`
	Winner     string   `json:"winner,omitempty"`
	Ambiguous  bool     `json:"ambiguous,omitempty"`
	Error      string   `json:"error,omitempty"`
}

type evidence struct {
	Title       string          `json:"title"`
	Artist      string          `json:"artist"`
	Album       string          `json:"album"`
	FileSeconds float64         `json:"file_seconds"`
	Sources     []sourceOutcome `json:"sources"`
}

type identity struct {
	Status        status
	Reason        string
	ISRC          string
	MBRecordingID string
	DeezerTrackID string
	AppleSongID   string
	Evidence      evidence
}

func outcomeOf(t libraryTrack, from source, found []candidate, lookupErr error) sourceOutcome {
	outcome := sourceOutcome{Source: from, Candidates: make([]judged, 0, len(found))}
	if lookupErr != nil {
		outcome.Error = lookupErr.Error()
		return outcome
	}
	for _, c := range found {
		outcome.Candidates = append(outcome.Candidates, judge(t, c))
	}
	winner, ambiguous := pickWinner(t.FileSeconds, outcome.Candidates)
	outcome.Winner, outcome.Ambiguous = winner, ambiguous
	return outcome
}

func pickWinner(fileSeconds float64, all []judged) (winnerID string, ambiguous bool) {
	guarded := slices.DeleteFunc(slices.Clone(all), func(j judged) bool { return !j.isGuarded() })
	if len(guarded) == 0 {
		return "", false
	}
	slices.SortFunc(guarded, func(a, b judged) int {
		return cmp.Or(
			cmp.Compare(math.Abs(a.LengthSeconds-fileSeconds), math.Abs(b.LengthSeconds-fileSeconds)),
			cmp.Compare(a.Distance, b.Distance),
			strings.Compare(a.ID, b.ID),
		)
	})
	if lengthSpread(guarded) > lengthToleranceSeconds {
		return "", true
	}
	return guarded[0].ID, false
}

func lengthSpread(candidates []judged) float64 {
	shortest, longest := candidates[0].LengthSeconds, candidates[0].LengthSeconds
	for _, c := range candidates {
		shortest, longest = min(shortest, c.LengthSeconds), max(longest, c.LengthSeconds)
	}
	return longest - shortest
}

func decide(t libraryTrack, outcomes []sourceOutcome) identity {
	found := identity{ISRC: canonicalISRC(t.ISRC), Evidence: evidence{
		Title: t.Title, Artist: t.Artist, Album: t.Album, FileSeconds: t.FileSeconds, Sources: outcomes,
	}}
	confirming := confirmingOutcomes(outcomes)
	switch {
	case len(confirming) > 0 && !hasAmbiguousSource(outcomes):
		found.Status = statusBound
		found.recordConfirmations(confirming)
	case t.hasUnreleasedAlbum() && answeredCleanlyWithNothing(outcomes):
		found.Status, found.Reason = statusUnofficial, reasonAlbumUnreleased
	default:
		found.Status, found.Reason = statusUnbound, unboundReason(t, outcomes)
	}
	return found
}

func hasAmbiguousSource(outcomes []sourceOutcome) bool {
	return slices.ContainsFunc(outcomes, func(o sourceOutcome) bool { return o.Ambiguous })
}

func answeredCleanlyWithNothing(outcomes []sourceOutcome) bool {
	return !slices.ContainsFunc(outcomes, func(o sourceOutcome) bool { return o.Error != "" || len(o.Candidates) > 0 })
}

func confirmingOutcomes(outcomes []sourceOutcome) []sourceOutcome {
	return slices.DeleteFunc(slices.Clone(outcomes), func(o sourceOutcome) bool { return o.Winner == "" })
}

func (i *identity) recordConfirmations(confirming []sourceOutcome) {
	names := make([]string, 0, len(confirming))
	for _, o := range confirming {
		names = append(names, string(o.Source))
		switch o.Source {
		case sourceDeezer:
			i.DeezerTrackID = o.Winner
		case sourceMusicBrainz:
			i.MBRecordingID = o.Winner
		case sourceApple:
			i.AppleSongID = o.Winner
		}
	}
	i.Reason = reasonBoundPrefix + strings.Join(names, "+")
}

func unboundReason(t libraryTrack, outcomes []sourceOutcome) string {
	if t.ISRC == "" {
		return reasonNoISRC
	}
	if hasAmbiguousSource(outcomes) {
		return reasonAmbiguous
	}
	if allFailed(outcomes) {
		return reasonLookupFailed
	}
	closest, anyCandidate := closestRefusal(outcomes)
	if !anyCandidate {
		return reasonISRCNotFound
	}
	return reasonRefusedPrefix + string(closest.Failures[0])
}

func allFailed(outcomes []sourceOutcome) bool {
	return len(outcomes) > 0 && !slices.ContainsFunc(outcomes, func(o sourceOutcome) bool { return o.Error == "" })
}

func closestRefusal(outcomes []sourceOutcome) (judged, bool) {
	var closest judged
	found := false
	for _, o := range outcomes {
		for _, j := range o.Candidates {
			if !found || len(j.Failures) < len(closest.Failures) || (len(j.Failures) == len(closest.Failures) && j.Distance < closest.Distance) {
				closest, found = j, true
			}
		}
	}
	return closest, found
}
