package main

import (
	"altune/go-api/internal/shared/songfacts"
	"cmp"
	"context"
	"fmt"
	"math"
	"slices"
)

const (
	reasonDifferingCutPrefix = "bound: file length differs from catalog by "
	minCatalogsAgreeingOnCut = 2
)

type differingCut struct {
	CatalogSeconds float64 `json:"catalog_seconds"`
	FileSeconds    float64 `json:"file_seconds"`
}

func (c differingCut) gap() float64 { return math.Abs(c.FileSeconds - c.CatalogSeconds) }

func (i *identity) noteLengthGap(fileSeconds, catalogSeconds float64) {
	gap := math.Abs(fileSeconds - catalogSeconds)
	if gap <= lengthToleranceSeconds {
		return
	}
	i.Reason = fmt.Sprintf("%s%.0fs", reasonDifferingCutPrefix, gap)
	i.Evidence.DifferingCut = &differingCut{CatalogSeconds: catalogSeconds, FileSeconds: fileSeconds}
}

func (i identity) hasUnlistedRecordingAtFileLength(listed []judged) bool {
	return slices.ContainsFunc(i.Evidence.ArtistFirst, func(o sourceOutcome) bool {
		return slices.ContainsFunc(o.Candidates, func(j judged) bool {
			return j.isGuarded() && !slices.ContainsFunc(listed, func(known judged) bool {
				return songfacts.CanonicalISRC(known.ISRC) == songfacts.CanonicalISRC(j.ISRC)
			})
		})
	})
}

func candidatesOf(outcomes []sourceOutcome) []judged {
	var all []judged
	for _, o := range outcomes {
		all = append(all, o.Candidates...)
	}
	return all
}

func isRefusedOnlyForLength(j judged) bool {
	return slices.Equal(j.Failures, []guard{guardLength})
}

func closestLengthOnlyRefusal(fileSeconds float64, o sourceOutcome) (judged, bool) {
	var closest judged
	found := false
	for _, j := range o.Candidates {
		if isRefusedOnlyForLength(j) && (!found || lengthGap(fileSeconds, j) < lengthGap(fileSeconds, closest)) {
			closest, found = j, true
		}
	}
	return closest, found
}

type catalogCut struct {
	source    source
	candidate judged
}

func catalogCuts(fileSeconds float64, outcomes []sourceOutcome) []catalogCut {
	var cuts []catalogCut
	for _, o := range outcomes {
		closest, ok := closestLengthOnlyRefusal(fileSeconds, o)
		if ok && o.Error == "" {
			cuts = append(cuts, catalogCut{source: o.Source, candidate: closest})
		}
	}
	slices.SortFunc(cuts, func(a, b catalogCut) int {
		return cmp.Compare(lengthGap(fileSeconds, a.candidate), lengthGap(fileSeconds, b.candidate))
	})
	return cuts
}

func agreeingOnCut(cuts []catalogCut) []catalogCut {
	if len(cuts) == 0 {
		return nil
	}
	anchor := cuts[0]
	return slices.DeleteFunc(slices.Clone(cuts), func(c catalogCut) bool {
		return math.Abs(c.candidate.LengthSeconds-anchor.candidate.LengthSeconds) > lengthToleranceSeconds
	})
}

type cutPolicy struct{ maxGapSeconds float64 }

func (p cutPolicy) allows(gap float64) bool { return p.maxGapSeconds == 0 || gap <= p.maxGapSeconds }

func (i *identity) refuseCut(fileSeconds, catalogSeconds float64) {
	i.Status, i.Reason = statusUnbound, reasonRefusedPrefix+string(guardLength)
	refused := &differingCut{CatalogSeconds: catalogSeconds, FileSeconds: fileSeconds}
	if kept := i.Evidence.DifferingCut; kept == nil || refused.gap() < kept.gap() {
		i.Evidence.DifferingCut = refused
	}
}

func (p cutPolicy) bindDifferingCut(_ context.Context, t libraryTrack, found identity) (identity, error) {
	if found.Status != statusUnbound || found.Reason != reasonRefusedPrefix+string(guardLength) || found.hasUnlistedRecordingAtFileLength(candidatesOf(found.Evidence.Sources)) {
		return found, nil
	}
	agreeing := agreeingOnCut(catalogCuts(t.FileSeconds, found.Evidence.Sources))
	if len(agreeing) < minCatalogsAgreeingOnCut {
		return found, nil
	}
	if catalogSeconds := agreeing[0].candidate.LengthSeconds; !p.allows(math.Abs(t.FileSeconds - catalogSeconds)) {
		found.refuseCut(t.FileSeconds, catalogSeconds)
		return found, nil
	}
	confirming := make([]sourceOutcome, 0, len(agreeing))
	for _, cut := range agreeing {
		confirming = append(confirming, sourceOutcome{Source: cut.source, Winner: cut.candidate.ID})
	}
	found.Status = statusBound
	found.recordConfirmations(confirming)
	found.noteLengthGap(t.FileSeconds, agreeing[0].candidate.LengthSeconds)
	return found, nil
}
